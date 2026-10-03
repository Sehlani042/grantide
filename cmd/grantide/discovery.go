package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Discovery contains paths only. It grants no operator or agent authority.
func writeDiscovery(path, dir, chromeProfile string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	data := map[string]string{"port_file": filepath.Join(absolute, "port"), "binary": binary}
	if chromeProfile != "" {
		data["chrome_user_data_dir"] = chromeProfile
	}
	// Preserve the paired profile across an ordinary restart without its optional flag.
	if old, err := os.ReadFile(path); err == nil && chromeProfile == "" {
		var previous map[string]string
		if json.Unmarshal(old, &previous) == nil && previous["port_file"] == data["port_file"] {
			data["chrome_user_data_dir"] = previous["chrome_user_data_dir"]
		}
	}
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".discovery-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
