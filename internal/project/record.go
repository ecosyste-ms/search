package project

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type Record struct {
	Package
	PackageID  int             `json:"package_id"`
	Namespace  *string         `json:"namespace"`
	Registry   RegistryRef     `json:"registry"`
	Repository json.RawMessage `json:"repository"`
}

func exportTime(s string) string {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999 MST"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	}
	return s
}

func (r Record) PackageRow() (Package, error) {
	p := r.Package
	p.ID = r.PackageID
	var repository map[string]json.RawMessage
	if err := json.Unmarshal(r.Repository, &repository); err != nil {
		return p, err
	}
	for _, key := range []string{"id"} {
		raw := repository[key]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var number json.Number
		if err := json.Unmarshal(raw, &number); err != nil {
			return p, fmt.Errorf("repository %s: %w", key, err)
		}
		n, err := number.Int64()
		if err != nil {
			return p, err
		}
		repository[key] = json.RawMessage(strconv.FormatInt(n, 10))
	}
	encoded, err := json.Marshal(repository)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(encoded, &p.Repo); err != nil {
		return p, err
	}
	p.Repo.Pushed, p.Repo.Synced = exportTime(p.Repo.Pushed), exportTime(p.Repo.Synced)
	return p, nil
}
