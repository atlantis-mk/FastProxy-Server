package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

func main() {
	repositoryID := flag.String("repository-id", "metacubex-meta-rules-dat", "built-in rule source repository id")
	output := flag.String("output", "", "snapshot JSON output path")
	flag.Parse()

	repo, ok := findBuiltInRepository(*repositoryID)
	if !ok {
		exitf("built-in repository %q was not found", *repositoryID)
	}

	index, err := buildIndexFromArchives(repo)
	if err != nil {
		index, err = repository.NewRuleSourceRepositoryBrowser().RefreshRemoteIndex(repo)
		if err != nil {
			exitf("refresh rule source index: %v", err)
		}
	}

	data, err := json.Marshal(index)
	if err != nil {
		exitf("marshal snapshot: %v", err)
	}
	data = append(data, '\n')

	if strings.TrimSpace(*output) == "" {
		_, _ = os.Stdout.Write(data)
		return
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		exitf("create output directory: %v", err)
	}
	if strings.HasSuffix(*output, ".gz") {
		data, err = gzipData(data)
		if err != nil {
			exitf("compress snapshot: %v", err)
		}
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		exitf("write snapshot: %v", err)
	}
}

func findBuiltInRepository(repositoryID string) (repository.RuleSourceRepository, bool) {
	for _, repo := range repository.BuiltInRuleSourceRepositories() {
		if repo.ID == repositoryID {
			return repo, true
		}
	}
	return repository.RuleSourceRepository{}, false
}

func buildIndexFromArchives(repo repository.RuleSourceRepository) (repository.RuleSourceIndex, error) {
	pathsByCore := map[repository.Core][]string{}
	for _, mapping := range repo.CoreMappings {
		paths, err := downloadArchivePaths(repo, mapping.Ref)
		if err != nil {
			return repository.RuleSourceIndex{}, err
		}
		pathsByCore[mapping.Core] = paths
	}
	return repository.BuildRuleSourceIndexFromFiles(repo, pathsByCore), nil
}

func downloadArchivePaths(repo repository.RuleSourceRepository, ref string) ([]string, error) {
	endpoint := fmt.Sprintf(
		"https://codeload.github.com/%s/%s/tar.gz/refs/heads/%s",
		repo.Owner,
		repo.Repository,
		ref,
	)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastProxy rule source snapshot generator")
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("archive request returned %s", resp.Status)
	}

	gzipReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	paths := []string{}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		pathParts := strings.SplitN(header.Name, "/", 2)
		if len(pathParts) != 2 {
			continue
		}
		paths = append(paths, pathParts[1])
	}
	return paths, nil
}

func gzipData(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func exitf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
