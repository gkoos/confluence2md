package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gkoos/confluence2md/internal/store"
)

func snapshotPageRecords(src map[string]store.PageRecord) map[string]store.PageRecord {
	out := make(map[string]store.PageRecord, len(src))
	maps.Copy(out, src)
	return out
}

func int64SliceToStringIDs(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, strconv.FormatInt(id, 10))
	}
	return out
}

func ensureLocalPageArtifact(outputDir string, record store.PageRecord, content string) (bool, error) {
	localPath := strings.TrimSpace(record.LocalPath)
	if localPath == "" {
		return false, fmt.Errorf("missing local_path for page %s", record.ID)
	}

	absPath := filepath.Join(outputDir, filepath.FromSlash(localPath))
	if _, err := os.Stat(absPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat local page artifact %s: %w", absPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return false, fmt.Errorf("create local page directory %s: %w", filepath.Dir(absPath), err)
	}

	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("write missing local page artifact %s: %w", absPath, err)
	}

	return true, nil
}
