package releasepack

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	Name             string            `json:"name"`
	SPDXID           string            `json:"SPDXID"`
	VersionInfo      string            `json:"versionInfo,omitempty"`
	DownloadLocation string            `json:"downloadLocation"`
	FilesAnalyzed    bool              `json:"filesAnalyzed"`
	LicenseConcluded string            `json:"licenseConcluded"`
	LicenseDeclared  string            `json:"licenseDeclared"`
	CopyrightText    string            `json:"copyrightText"`
	ExternalRefs     []spdxExternalRef `json:"externalRefs,omitempty"`
	Checksums        []spdxChecksum    `json:"checksums,omitempty"`
	PrimaryPurpose   string            `json:"primaryPackagePurpose,omitempty"`
	SourceInfo       string            `json:"sourceInfo,omitempty"`
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
			DownloadLocation: "NOASSERTION",
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
		} else {
			sum := resolved.Sum
			if sum == "" {
				sum = module.Sum
			}
			if checksum, ok := goSumSHA256(sum); ok {
				entry.Checksums = []spdxChecksum{{Algorithm: "SHA256", ChecksumValue: checksum}}
			}
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
	for {
		var module goModule
		if err := decoder.Decode(&module); err == io.EOF {
			break
		} else if err != nil {
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

func modulePURL(path, version string) string {
	locator := "pkg:golang/" + path
	if version != "" {
		locator += "@" + version
	}
	return locator
}

func goSumSHA256(sum string) (string, bool) {
	if !strings.HasPrefix(sum, "h1:") {
		return "", false
	}
	digest, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sum, "h1:"))
	if err != nil || len(digest) != 32 {
		return "", false
	}
	return hex.EncodeToString(digest), true
}
