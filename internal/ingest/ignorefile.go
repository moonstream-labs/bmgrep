package ingest

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ReadIgnorePatterns returns non-empty, non-comment patterns in file order.
func ReadIgnorePatterns(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open ignore file: %w", err)
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read ignore file: %w", err)
	}
	return patterns, nil
}

// AppendIgnorePatterns appends one pattern per line to an ignore file.
func AppendIgnorePatterns(path string, patterns []string) error {
	if len(patterns) == 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("open ignore file for append: %w", err)
	}
	defer f.Close()

	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := f.WriteString(p + "\n"); err != nil {
			return fmt.Errorf("append ignore pattern %q: %w", p, err)
		}
	}
	return nil
}

// RemoveIgnorePatterns rewrites an ignore file without the provided patterns.
func RemoveIgnorePatterns(path string, remove []string) error {
	if len(remove) == 0 {
		return nil
	}

	removeSet := make(map[string]bool, len(remove))
	for _, p := range remove {
		p = strings.TrimSpace(p)
		if p != "" {
			removeSet[p] = true
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read ignore file for remove: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if removeSet[trimmed] {
			continue
		}
		out = append(out, line)
	}

	content := strings.Join(out, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write ignore file after remove: %w", err)
	}
	return nil
}
