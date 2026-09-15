package workflow

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func LoadAll(path string) (map[string]*Definition, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var files []string
	if info.IsDir() {
		err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".yaml" || ext == ".yml" {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		files = []string{path}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no workflow YAML files found in %s", path)
	}

	out := make(map[string]*Definition, len(files))
	for _, file := range files {
		def, err := Load(file)
		if err != nil {
			return nil, err
		}
		if existing, ok := out[def.Name]; ok {
			return nil, fmt.Errorf("duplicate workflow name %q (from %s)", existing.Name, file)
		}
		out[def.Name] = def
	}
	return out, nil
}
