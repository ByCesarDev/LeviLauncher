package packages

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/liteldev/LeviLauncher/internal/utils"
)

type RawManifest struct {
	FormatVersion int `json:"format_version"`
	Header        struct {
		Name             string      `json:"name"`
		Description      string      `json:"description"`
		UUID             string      `json:"uuid"`
		Version          interface{} `json:"version"`            // []int or string
		MinEngineVersion interface{} `json:"min_engine_version"` // []int or string
	} `json:"header"`
	Modules []struct {
		Type    string      `json:"type"`
		UUID    string      `json:"uuid"`
		Version interface{} `json:"version"` // []int or string
	} `json:"modules"`
}

func (pm *PackManager) LoadPacksForVersion(versionName string, resourcePacksDir, behaviorPacksDir string, skinPacksDirs ...string) ([]Pack, error) {
	var packs []Pack

	scanDir := func(dir string, defaultType PackType, isDev bool) {
		if !utils.DirExists(dir) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		for _, e := range entries {
			startPath := filepath.Join(dir, e.Name())
			lower := strings.ToLower(e.Name())

			if !e.IsDir() {
				if strings.HasSuffix(lower, ".mcpack") || strings.HasSuffix(lower, ".zip") {
					manifest, err := parseZipManifest(startPath, defaultType)
					if err != nil {
						base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
						manifest = PackManifest{
							Name:        base,
							Description: "Archivo de paquete (" + filepath.Ext(e.Name()) + ")",
							PackType:    defaultType,
							Location:    startPath,
							Identity: PackIdVersion{
								PackType: defaultType,
							},
						}
					}
					manifest.IsDevelopment = isDev
					packs = append(packs, Pack{
						Manifest:      manifest,
						Path:          startPath,
						IsDevelopment: isDev,
					})
				}
				continue
			}

			var findPackRoot func(current string, depth int) string
			findPackRoot = func(current string, depth int) string {
				if depth > 3 {
					return ""
				}

				mPath := filepath.Join(current, "manifest.json")
				if utils.FileExists(mPath) {
					return current
				}

				subs, err := os.ReadDir(current)
				if err != nil {
					return ""
				}
				for _, sub := range subs {
					if sub.IsDir() {
						found := findPackRoot(filepath.Join(current, sub.Name()), depth+1)
						if found != "" {
							return found
						}
					}
				}
				return ""
			}

			packPath := findPackRoot(startPath, 0)
			if packPath == "" {
				// Carpeta vacía o sin manifest.json: se muestra de respaldo
				packs = append(packs, Pack{
					Manifest: PackManifest{
						Name:        e.Name(),
						Description: "Carpeta sin manifest.json",
						PackType:    defaultType,
						Location:    startPath,
						Identity: PackIdVersion{
							PackType: defaultType,
						},
						IsDevelopment: isDev,
					},
					Path:          startPath,
					IsDevelopment: isDev,
				})
				continue
			}

			manifestPath := filepath.Join(packPath, "manifest.json")
			manifest, err := parseManifest(manifestPath, defaultType)
			if err != nil {
				manifest = PackManifest{
					Name:        e.Name(),
					Description: "Error al leer manifest.json",
					PackType:    defaultType,
					Location:    packPath,
					Identity: PackIdVersion{
						PackType: defaultType,
					},
					IsDevelopment: isDev,
				}
			}
			manifest.IsDevelopment = isDev
			packs = append(packs, Pack{
				Manifest:      manifest,
				Path:          packPath,
				IsDevelopment: isDev,
			})
		}
	}

	scanDir(resourcePacksDir, PackTypeResources, false)
	if resourcePacksDir != "" {
		devRes := filepath.Join(filepath.Dir(resourcePacksDir), "development_resource_packs")
		scanDir(devRes, PackTypeResources, true)
	}

	scanDir(behaviorPacksDir, PackTypeBehavior, false)
	if behaviorPacksDir != "" {
		devBp := filepath.Join(filepath.Dir(behaviorPacksDir), "development_behavior_packs")
		scanDir(devBp, PackTypeBehavior, true)
	}

	for _, dir := range skinPacksDirs {
		scanDir(dir, PackTypeSkins, false)
		if dir != "" {
			devSkins := filepath.Join(filepath.Dir(dir), "development_skin_packs")
			scanDir(devSkins, PackTypeSkins, true)
		}
	}

	pm.mu.Lock()
	pm.packs[versionName] = packs
	pm.mu.Unlock()
	return packs, nil
}

func parseManifest(path string, defaultType PackType) (PackManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return PackManifest{}, err
	}

	b = utils.JsonCompatBytes(b)

	var raw RawManifest
	if err := json.Unmarshal(b, &raw); err != nil {
		return PackManifest{}, err
	}

	pm := PackManifest{}

	pm.Identity.UUID = raw.Header.UUID

	version := ParseVersion(raw.Header.Version)
	if len(version) >= 3 {
		pm.Identity.Version = SemVersion{
			Major: version[0],
			Minor: version[1],
			Patch: version[2],
		}
	}

	pm.PackType = defaultType
	pm.Identity.PackType = defaultType

	for _, mod := range raw.Modules {
		switch strings.ToLower(strings.TrimSpace(mod.Type)) {
		case "resources":
			if defaultType == PackTypeResources || pm.PackType == PackTypeInvalid {
				pm.PackType = PackTypeResources
			}
		case "data", "client_data", "script", "javascript":
			if defaultType == PackTypeBehavior || pm.PackType == PackTypeInvalid {
				pm.PackType = PackTypeBehavior
			}
		case "skin_pack":
			pm.PackType = PackTypeSkins
		}
	}
	if pm.PackType == PackTypeInvalid {
		pm.PackType = defaultType
	}
	pm.Identity.PackType = pm.PackType

	minEngineVersion := ParseVersion(raw.Header.MinEngineVersion)
	if len(minEngineVersion) >= 3 {
		pm.MinEngineVersion = MinEngineVersion{
			SemVersion: SemVersion{
				Major: minEngineVersion[0],
				Minor: minEngineVersion[1],
				Patch: minEngineVersion[2],
			},
		}
	}

	pm.Name = raw.Header.Name
	pm.Description = raw.Header.Description
	pm.Location = filepath.Dir(path)

	texts := ReadPackTexts(pm.Location)
	if texts != nil {
		if val, ok := texts[pm.Name]; ok {
			pm.Name = val
		}
		if val, ok := texts[pm.Description]; ok {
			pm.Description = val
		}
	}

	iconPath := filepath.Join(filepath.Dir(path), "pack_icon.png")
	if utils.FileExists(iconPath) {
		pm.PackIconLocation = iconPath
	}

	return pm, nil
}

func ReadPackTexts(root string) map[string]string {
	textsDir := filepath.Join(root, "texts")
	if !utils.DirExists(textsDir) {
		return nil
	}
	langFile := filepath.Join(textsDir, "en_US.lang")
	if !utils.FileExists(langFile) {
		lj := filepath.Join(textsDir, "languages.json")
		if utils.FileExists(lj) {
			if b, err := os.ReadFile(lj); err == nil {
				var langs []string
				_ = json.Unmarshal(utils.JsonCompatBytes(b), &langs)
				if len(langs) > 0 {
					cand := filepath.Join(textsDir, strings.TrimSpace(langs[0])+".lang")
					if utils.FileExists(cand) {
						langFile = cand
					}
				}
			}
		}
	}
	if !utils.FileExists(langFile) {
		ents, err := os.ReadDir(textsDir)
		if err == nil {
			for _, e := range ents {
				if e.IsDir() {
					continue
				}
				name := strings.ToLower(strings.TrimSpace(e.Name()))
				if strings.HasSuffix(name, ".lang") {
					langFile = filepath.Join(textsDir, e.Name())
					break
				}
			}
		}
	}
	if !utils.FileExists(langFile) {
		return nil
	}
	b, err := os.ReadFile(langFile)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	m := make(map[string]string, 16)
	for _, ln := range lines {
		l := strings.TrimSpace(strings.TrimRight(ln, "\r"))
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if idx := strings.IndexRune(l, '#'); idx >= 0 {
			l = strings.TrimSpace(l[:idx])
		}
		if l == "" {
			continue
		}
		if eq := strings.IndexRune(l, '='); eq >= 0 {
			k := strings.TrimSpace(l[:eq])
			v := strings.TrimSpace(l[eq+1:])
			if k != "" {
				m[k] = v
			}
		}
	}
	return m
}

func ParseVersion(v interface{}) []int {
	switch val := v.(type) {
	case []interface{}:
		res := make([]int, len(val))
		for i, x := range val {
			if f, ok := x.(float64); ok {
				res[i] = int(f)
			}
		}
		return res
	case []int:
		return val
	case string:
		parts := strings.Split(val, ".")
		res := make([]int, 0, len(parts))
		for _, p := range parts {
			if i, err := strconv.Atoi(p); err == nil {
				res = append(res, i)
			}
		}
		return res
	}
	return nil
}


func parseZipManifest(zipPath string, defaultType PackType) (PackManifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return PackManifest{}, err
	}
	defer zr.Close()

	var manifestFile *zip.File
	for _, f := range zr.File {
		clean := filepath.ToSlash(f.Name)
		if strings.EqualFold(clean, "manifest.json") || strings.HasSuffix(strings.ToLower(clean), "/manifest.json") {
			manifestFile = f
			break
		}
	}
	if manifestFile == nil {
		return PackManifest{}, fmt.Errorf("no manifest.json found in zip")
	}

	rc, err := manifestFile.Open()
	if err != nil {
		return PackManifest{}, err
	}
	defer rc.Close()

	b, err := io.ReadAll(rc)
	if err != nil {
		return PackManifest{}, err
	}

	b = utils.JsonCompatBytes(b)

	var raw RawManifest
	if err := json.Unmarshal(b, &raw); err != nil {
		return PackManifest{}, err
	}

	pm := PackManifest{}
	pm.Identity.UUID = raw.Header.UUID

	version := ParseVersion(raw.Header.Version)
	if len(version) >= 3 {
		pm.Identity.Version = SemVersion{
			Major: version[0],
			Minor: version[1],
			Patch: version[2],
		}
	}

	pm.PackType = defaultType
	for _, mod := range raw.Modules {
		switch strings.ToLower(strings.TrimSpace(mod.Type)) {
		case "resources":
			if defaultType == PackTypeResources || pm.PackType == PackTypeInvalid {
				pm.PackType = PackTypeResources
			}
		case "data", "client_data", "script", "javascript":
			if defaultType == PackTypeBehavior || pm.PackType == PackTypeInvalid {
				pm.PackType = PackTypeBehavior
			}
		case "skin_pack":
			pm.PackType = PackTypeSkins
		}
	}
	if pm.PackType == PackTypeInvalid {
		pm.PackType = defaultType
	}
	pm.Identity.PackType = pm.PackType

	pm.Name = raw.Header.Name
	if pm.Name == "" || pm.Name == "pack.name" {
		base := filepath.Base(zipPath)
		pm.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	pm.Description = raw.Header.Description
	pm.Location = zipPath
	return pm, nil
}
