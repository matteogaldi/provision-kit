package workflow

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

func Parse(r io.Reader) (*Definition, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data)
}

func ParseBytes(data []byte) (*Definition, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var def Definition
	if err := dec.Decode(&def); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	if def.Services == nil {
		def.Services = map[string]string{}
	}
	if def.Inputs == nil {
		def.Inputs = map[string]InputSpec{}
	}
	for i := range def.Steps {
		if def.Steps[i].DependsOn == nil {
			def.Steps[i].DependsOn = []string{}
		}
		if def.Steps[i].Request.Headers == nil {
			def.Steps[i].Request.Headers = map[string]string{}
		}
	}
	return &def, nil
}

func ParseFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	def, err := ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return def, nil
}

func Load(path string) (*Definition, error) {
	def, err := ParseFile(path)
	if err != nil {
		return nil, err
	}
	if err := Validate(def); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return def, nil
}
