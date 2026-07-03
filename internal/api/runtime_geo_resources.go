package api

import (
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed runtime_geo_resources/*.gz
var runtimeGeoResourceFS embed.FS

var embeddedMihomoGeoResources = []embeddedRuntimeResource{
	{Name: "geoip.dat", Path: "runtime_geo_resources/geoip.dat.gz"},
	{Name: "geosite.dat", Path: "runtime_geo_resources/geosite.dat.gz"},
	{Name: "country.mmdb", Path: "runtime_geo_resources/country.mmdb.gz"},
	{Name: "GeoLite2-ASN.mmdb", Path: "runtime_geo_resources/GeoLite2-ASN.mmdb.gz"},
}

type embeddedRuntimeResource struct {
	Name string
	Path string
}

func ensureEmbeddedMihomoGeoResources(runtimeDir string) error {
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return err
	}
	for _, resource := range embeddedMihomoGeoResources {
		target := filepath.Join(runtimeDir, resource.Name)
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			continue
		} else if err == nil {
			return fmt.Errorf("runtime resource %s is a directory", resource.Name)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := extractEmbeddedRuntimeResource(resource, target); err != nil {
			return fmt.Errorf("extract runtime resource %s: %w", resource.Name, err)
		}
	}
	return nil
}

func extractEmbeddedRuntimeResource(resource embeddedRuntimeResource, target string) error {
	data, err := runtimeGeoResourceFS.Open(resource.Path)
	if err != nil {
		return err
	}
	defer data.Close()

	reader, err := gzip.NewReader(data)
	if err != nil {
		return err
	}
	defer reader.Close()

	tmp := target + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, reader)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
