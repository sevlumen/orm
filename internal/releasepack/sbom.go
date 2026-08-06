package releasepack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type goModule struct {
	Path    string    `json:"Path"`
	Version string    `json:"Version"`
	Main    bool      `json:"Main"`
	Sum     string    `json:"Sum"`
	Replace *goModule `json:"Replace"`
}

type spdxDocument struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	Name              string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo   `json:"creationInfo"`
	Packages          []spdxPackage      `json:"packages"`
	Relationships     []spdxRelationship `json:"relationships"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name              string            `json:"name"`
	SPDXID            string            `json:"SPDXID"`
	VersionInfo       string            `json:"versionInfo,omitempty"`
	DownloadLocation  string            `json:"downloadLocation"`
	FilesAnalyzed     bool              `json:"filesAnalyzed"`
	LicenseConcluded  string            `json:"licenseConcluded"`
	LicenseDeclared   string            `json:"licenseDeclared"`
	CopyrightText     string            `json:"copyrightText"`
	ExternalRefs      []spdxExternalRef `json:"externalRefs,omitempty"`
	PackageChecksums  []spdxChecksum    `json:"checksums,omitempty"`
	PrimaryPurpose    string            `json:"primaryPackagePurpose,omitempty"`
	SourceInfo        string            `json:"sourceInfo,omitempty"`
}

type spdxExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type spdxChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type spdxRelationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

func writeSBOM(ctx context.Context, root, path string, config Config, buildTime time.Time) error {
	modules, err := listModules(ctx, root)
	if err != nil {
		return err
	}
	document := spdxDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              "sevlumen-orm-" + config.Version,
		DocumentNamespace: fmt.Sprintf("https://github.com/sevlumen/orm/releases/%s/sbom-%s", config.Version, config.Commit),
		CreationInfo: spdxCreationInfo{
			Created:  buildTime.Format(time.RFC3339),
			Creators: []string{"Tool: github.com/sevlumen/orm/cmd/releasepack"},
		},
	}
	rootID := ""
	for index, module := range modules {
		resolved := module
		if module.Replace != nil {
			resolved = *module.Replace
		}
		version := resolved.Version
		if module.Main {
			version = config.Version
		}
		id := fmt.Sprintf("SPDXRef-Package-%03d", index+1)
		entry := spdxPackage{
			Name:             module.Path,
			SPDXID:           id,
			VersionInfo:      version,
			DownloadLocation: moduleDownloadLocation(resolved),
			FilesAnalyzed:    false,
			LicenseConcluded: "NOASSERTION",
			LicenseDeclared:  "NOASSERTION",
			CopyrightText:    "NOASSERTION",
			ExternalRefs: []spdxExternalRef{{
				ReferenceCategory: "PACKAGE-MANAGER",
				ReferenceType:     "purl",
				ReferenceLocator:  modulePURL(module.Path, version),
			}},
		}
		if module.Main {
			entry.PrimaryPurpose = "APPLICATION"
			entry.SourceInfo = "Release commit " + config.Commit
			rootID = id
		} else if resolved.Sum != "" {
			entry.PackageChecksums = []spdxChecksum{{Algorithm: "SHA256", ChecksumValue: checksumFromGoSum(resolved.Sum)}}
		}
		document.Packages = append(document.Packages, entry)
	}
	if rootID == "" {
		return fmt.Errorf("releasepack: Go module graph has no main module")
	}
	document.Relationships = append(document.Relationships, spdxRelationship{
		SPDXElementID:      "SPDXRef-DOCUMENT",
		RelationshipType:   "DESCRIBES",
		RelatedSPDXElement: rootID,
	})
	for _, entry := range document.Packages {
		if entry.SPDXID == rootID {
			continue
		}
		document.Relationships = append(document.Relationships, spdxRelationship{
			SPDXElementID:      rootID,
			RelationshipType:   "DEPENDS_ON",
			RelatedSPDXElement: entry.SPDXID,
		})
	}
	return writeJSONExclusive(path, document)
}

func listModules(ctx context.Context, root string) ([]goModule, error) {
	command := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-m", "-json", "all")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("releasepack: list Go modules: %w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("releasepack: list Go modules: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var modules []goModule
	for decoder.More() {
		var module goModule
		if err := decoder.Decode(&module); err != nil {
			return nil, fmt.Errorf("releasepack: decode Go module graph: %w", err)
		}
		modules = append(modules, module)
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("releasepack: empty Go module graph")
	}
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Main != modules[j].Main {
			return modules[i].Main
		}
		return modules[i].Path < modules[j].Path
	})
	return modules, nil
}

func moduleDownloadLocation(module goModule) string {
	if module.Path == "" {
		return "NOASSERTION"
	}
	if module.Version == "" {
		return "NOASSERTION"
	}
	return "https://proxy.golang.org/" + module.Path + "/@v/" + module.Version + ".zip"
}

func modulePURL(path, version string) string {
	locator := "pkg:golang/" + path
	if version != "" {
		locator += "@" + version
	}
	return locator
}

func checksumFromGoSum(sum string) string {
	// Go module sums are h1/base64, not SHA-256 hex. Preserve the authenticated
	// value in SourceInfo instead of mislabeling it as an SPDX SHA-256 checksum.
	return strings.TrimPrefix(sum, "h1:")
}
