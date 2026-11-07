package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const trafficHistoryFileVersion = 1

type trafficHistoryFile struct {
	Version int                       `json:"version"`
	Entries []trafficHistoryDiskEntry `json:"entries"`
}

type trafficHistoryDiskEntry struct {
	Day      string `json:"day"`
	User     string `json:"user"`
	Upstream string `json:"upstream"`
	Target   string `json:"target"`
	BytesIn  int64  `json:"bytes_in"`
	BytesOut int64  `json:"bytes_out"`
}

func loadTrafficHistory(path string) ([]trafficHistoryDiskEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	var file trafficHistoryFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode traffic history: %w", err)
	}
	if file.Version > trafficHistoryFileVersion {
		return nil, fmt.Errorf("unsupported traffic history version %d", file.Version)
	}
	return file.Entries, nil
}

func writeTrafficHistory(path string, entries []trafficHistoryDiskEntry) error {
	payload, err := json.Marshal(trafficHistoryFile{
		Version: trafficHistoryFileVersion,
		Entries: entries,
	})
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".traffic-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
