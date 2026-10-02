package process

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func packageRoots(prefix string) []string {
	roots := []string{filepath.Join(prefix, "node_modules"), filepath.Join(prefix, "global", "node_modules")}
	versions, _ := os.ReadDir(filepath.Join(prefix, "global"))
	for _, version := range versions {
		roots = append(roots, filepath.Join(prefix, "global", version.Name(), "node_modules"))
	}
	return roots
}

func packageDirs(root string) []string {
	entries, _ := os.ReadDir(root)
	var dirs []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if strings.HasPrefix(entry.Name(), "@") {
			scoped, _ := os.ReadDir(dir)
			for _, pkg := range scoped {
				dirs = append(dirs, filepath.Join(dir, pkg.Name()))
			}
		} else {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func declaredBins(dir, name string) []string {
	f, err := os.Open(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var pkg struct {
		Name string          `json:"name"`
		Bin  json.RawMessage `json:"bin"`
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, &pkg) != nil {
		return nil
	}
	var bins map[string]string
	if json.Unmarshal(pkg.Bin, &bins) != nil {
		var entry string
		if json.Unmarshal(pkg.Bin, &entry) != nil {
			return nil
		}
		bins = map[string]string{path.Base(pkg.Name): entry}
	}
	var out []string
	for bin, entry := range bins {
		if !strings.EqualFold(bin, name) || entry == "" || filepath.IsAbs(entry) || filepath.VolumeName(entry) != "" {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(entry))
		rel, err := filepath.Rel(dir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
			out = append(out, target)
		}
	}
	return out
}

// Match literal file references, not batch control flow. In particular, package
// metadata alone must not select a stale or shadowed package with the same bin name.
func shimReferences(source, prefix, entry string) bool {
	paths := []string{entry}
	if real, err := filepath.EvalSymlinks(entry); err == nil {
		paths = append(paths, real)
	}
	for _, file := range paths {
		variants := []string{file}
		if rel, err := filepath.Rel(prefix, file); err == nil {
			rel = filepath.ToSlash(rel)
			variants = append(variants, "%dp0%/"+rel, "%~dp0/"+rel, "%~dp0"+rel)
		}
		for _, variant := range variants {
			if strings.Contains(source, strings.ToLower(filepath.ToSlash(variant))+`"`) {
				return true
			}
		}
	}
	return false
}

func resolvePackageBin(shim string) (string, error) {
	f, err := os.Open(shim)
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<10))
	f.Close()
	if err != nil {
		return "", err
	}
	source := strings.ToLower(strings.ReplaceAll(string(data), `\`, "/"))
	prefix := filepath.Dir(shim)
	name := strings.TrimSuffix(filepath.Base(shim), filepath.Ext(shim))
	found := map[string]string{}
	for _, root := range packageRoots(prefix) {
		for _, dir := range packageDirs(root) {
			for _, entry := range declaredBins(dir, name) {
				if !shimReferences(source, prefix, entry) {
					continue
				}
				real, err := filepath.EvalSymlinks(entry)
				if err != nil {
					continue
				}
				found[strings.ToLower(real)] = entry
			}
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("%s must reference exactly one installed npm/pnpm bin; custom batch launchers are not supported", shim)
	}
	var entry string
	for _, candidate := range found {
		entry = candidate
	}
	return entry, nil
}

func binRuntime(entry string) (string, []string, error) {
	if strings.EqualFold(filepath.Ext(entry), ".exe") {
		return "", nil, nil
	}
	f, err := os.Open(entry)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	line, _ := bufio.NewReader(io.LimitReader(f, 4096)).ReadString('\n')
	if strings.HasPrefix(line, "#!") {
		words := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#!")))
		if len(words) > 0 && path.Base(words[0]) == "env" {
			words = words[1:]
			if len(words) > 0 && words[0] == "-S" {
				words = words[1:]
			}
		}
		if len(words) > 0 && (path.Base(words[0]) == "node" || path.Base(words[0]) == "bun") {
			for _, arg := range words[1:] {
				if strings.ContainsAny(arg, `'"\`) {
					return "", nil, fmt.Errorf("unsupported quoted runtime arguments in %s", entry)
				}
			}
			return path.Base(words[0]), words[1:], nil
		}
		return "", nil, fmt.Errorf("unsupported bin interpreter in %s", entry)
	}
	switch strings.ToLower(filepath.Ext(entry)) {
	case ".js", ".cjs", ".mjs":
		return "node", nil, nil
	}
	return "", nil, fmt.Errorf("%s is neither a native executable nor a Node/Bun entry", entry)
}
