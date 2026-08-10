package compat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

type Manifest struct {
	Version   int        `json:"version"`
	Contracts []Contract `json:"contracts"`
}

type Contract struct {
	ID        string   `json:"id"`
	Surface   string   `json:"surface"`
	Owner     string   `json:"owner"`
	Platforms []string `json:"platforms"`
	Status    string   `json:"status"`
}

type Counts struct {
	Implemented int `json:"implemented"`
	InProgress  int `json:"in_progress"`
	Pending     int `json:"pending"`
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if err = manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported coverage manifest version %d", m.Version)
	}
	if len(m.Contracts) == 0 {
		return errors.New("coverage manifest has no contracts")
	}
	seen := map[string]bool{}
	for _, contract := range m.Contracts {
		if contract.ID == "" || contract.Surface == "" || contract.Owner == "" || len(contract.Platforms) == 0 {
			return fmt.Errorf("coverage contract is incomplete: %+v", contract)
		}
		if seen[contract.ID] {
			return fmt.Errorf("duplicate coverage contract %q", contract.ID)
		}
		seen[contract.ID] = true
		switch contract.Status {
		case "implemented", "in-progress", "pending":
		default:
			return fmt.Errorf("coverage contract %q has invalid status %q", contract.ID, contract.Status)
		}
	}
	return nil
}

func (m Manifest) Counts() Counts {
	var counts Counts
	for _, contract := range m.Contracts {
		switch contract.Status {
		case "implemented":
			counts.Implemented++
		case "in-progress":
			counts.InProgress++
		case "pending":
			counts.Pending++
		}
	}
	return counts
}

func (m Manifest) IDs() []string {
	ids := make([]string, 0, len(m.Contracts))
	for _, contract := range m.Contracts {
		ids = append(ids, contract.ID)
	}
	sort.Strings(ids)
	return ids
}
