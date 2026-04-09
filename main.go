package main

import (
"bufio"
"fmt"
"io"
"net/http"
"os"
"os/exec"
"path/filepath"
"strings"

"gopkg.in/yaml.v3"
)

const (
sourceURL   = "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat_plain.yml"
dataDir     = "data"
combinedOut = "dlc.yaml"
)

// Input format from v2fly domain-list-community
type DLCFile struct {
Lists []DLCList `yaml:"lists"`
}

type DLCList struct {
Name   string   `yaml:"name"`
Length int      `yaml:"length"`
Rules  []string `yaml:"rules"`
}

// parsedRule holds the converted representations of a single v2fly rule.
type parsedRule struct {
// yamlEntry is written into the per-list domain-behavior YAML.
// For domain/full rules: "+.example.com" / "example.com".
// For regexp rules: "DOMAIN-REGEX,^pattern" (kept for YAML reference;
// mihomo warns and skips these when converting to MRS).
yamlEntry string
// classical is the Mihomo classical format used in the combined dlc.yaml.
classical string
}

// parseRule converts a raw v2fly rule string into a parsedRule.
// Strips attributes (e.g. ":@cn") and maps rule types.
// Returns false if the rule type is unknown.
func parseRule(raw string) (parsedRule, bool) {
raw = strings.Trim(raw, `"`)

// Strip attribute suffix like ":@ads"
if idx := strings.LastIndex(raw, ":@"); idx != -1 {
raw = raw[:idx]
}

parts := strings.SplitN(raw, ":", 2)
if len(parts) != 2 {
return parsedRule{}, false
}
ruleType, value := parts[0], parts[1]

switch ruleType {
case "domain":
return parsedRule{
yamlEntry: "+." + value,
classical: "DOMAIN-SUFFIX," + value,
}, true
case "full":
return parsedRule{
yamlEntry: value,
classical: "DOMAIN," + value,
}, true
case "regexp":
// Kept in per-list YAML for reference; mihomo skips it when generating MRS.
return parsedRule{
yamlEntry: "DOMAIN-REGEX," + value,
classical: "DOMAIN-REGEX," + value,
}, true
default:
return parsedRule{}, false
}
}

func fetchSource() ([]byte, error) {
fmt.Println("Fetching source from", sourceURL)
resp, err := http.Get(sourceURL) //nolint:noctx
if err != nil {
return nil, fmt.Errorf("fetch failed: %w", err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusOK {
return nil, fmt.Errorf("unexpected status: %s", resp.Status)
}
return io.ReadAll(resp.Body)
}

// writeYAML writes a YAML rule-set file with a payload list.
func writeYAML(path string, entries []string) error {
if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
return err
}
f, err := os.Create(path)
if err != nil {
return err
}
defer f.Close()

w := bufio.NewWriter(f)
fmt.Fprintln(w, "payload:")
for _, e := range entries {
fmt.Fprintf(w, "  - %s\n", e)
}
return w.Flush()
}

// convertToMRS generates an MRS file from a domain-behavior YAML file.
// DOMAIN-REGEX entries in the YAML are silently skipped by mihomo.
func convertToMRS(yamlPath, mrsPath string) error {
cmd := exec.Command("mihomo", "convert-ruleset", "domain", "yaml", yamlPath, mrsPath)
cmd.Stderr = os.Stderr
return cmd.Run()
}

func main() {
data, err := fetchSource()
if err != nil {
fmt.Fprintln(os.Stderr, "Error:", err)
os.Exit(1)
}

var dlc DLCFile
if err := yaml.Unmarshal(data, &dlc); err != nil {
fmt.Fprintln(os.Stderr, "Error parsing YAML:", err)
os.Exit(1)
}

fmt.Printf("Parsed %d lists\n", len(dlc.Lists))

var allClassical []string

for _, list := range dlc.Lists {
var yamlEntries []string
var classicalRules []string

for _, raw := range list.Rules {
pr, ok := parseRule(raw)
if !ok {
continue
}
yamlEntries = append(yamlEntries, pr.yamlEntry)
classicalRules = append(classicalRules, pr.classical)
}

if len(yamlEntries) == 0 {
continue
}

allClassical = append(allClassical, classicalRules...)

listDir := filepath.Join(dataDir, list.Name)
yamlPath := filepath.Join(listDir, list.Name+".yaml")
mrsPath := filepath.Join(listDir, list.Name+".mrs")

if err := writeYAML(yamlPath, yamlEntries); err != nil {
fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", yamlPath, err)
os.Exit(1)
}

if err := convertToMRS(yamlPath, mrsPath); err != nil {
fmt.Fprintf(os.Stderr, "Error converting %s to MRS: %v\n", list.Name, err)
os.Exit(1)
}

fmt.Printf("  [OK] %-30s %d rules\n", list.Name, len(yamlEntries))
}

// Write combined classical YAML with all rules (including DOMAIN-REGEX)
if err := writeYAML(combinedOut, allClassical); err != nil {
fmt.Fprintln(os.Stderr, "Error writing combined YAML:", err)
os.Exit(1)
}
fmt.Printf("\nCombined YAML written to %s (%d total rules)\n", combinedOut, len(allClassical))
}
