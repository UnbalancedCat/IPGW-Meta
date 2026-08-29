package candidate

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"strings"
)

const candidateModulePath = "github.com/UnbalancedCat/ipgw-meta"

// validateGoBinary binds a frozen output to candidate-v1's command package,
// target, Go version, and reproducibility settings. The Go build-info format
// does not retain the literal -ldflags argument, so the corresponding
// observable invariants are checked: stripped symbol/debug tables and, for
// products, the injected release version bytes.
func validateGoBinary(content []byte, target, command string, helper bool, expectedGoVersion string) error {
	goos, goarch, ok := splitBuildTarget(target)
	if !ok || expectedGoVersion == "" || len(content) == 0 || int64(len(content)) > MaxBinaryBytes {
		return ErrInvalidInput
	}
	info, err := buildinfo.Read(bytes.NewReader(content))
	if err != nil || info.GoVersion != expectedGoVersion {
		return ErrInvalidInput
	}
	expectedPath := candidateModulePath + "/cmd/" + command
	if helper {
		if command != "ipgw-live-gate" {
			return ErrInvalidInput
		}
		expectedPath = candidateModulePath + "/internal/cmd/ipgw-live-gate"
	} else if command != "ipgw" && command != "ipgw-meta" && command != "ipgw-legacy" {
		return ErrInvalidInput
	}
	if info.Path != expectedPath || info.Main.Path != candidateModulePath ||
		info.Main.Version != "(devel)" || info.Main.Sum != "" || info.Main.Replace != nil {
		return ErrInvalidInput
	}

	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		if setting.Key == "" || strings.HasPrefix(setting.Key, "vcs") {
			return ErrInvalidInput
		}
		if _, duplicate := settings[setting.Key]; duplicate {
			return ErrInvalidInput
		}
		switch setting.Key {
		case "-buildmode", "-compiler", "-trimpath", "CGO_ENABLED", "GOARCH", "GOOS", "GOAMD64", "GOARM64", "DefaultGODEBUG":
			settings[setting.Key] = setting.Value
		default:
			// Non-default experiments, tags, race instrumentation, and other
			// uncontracted build settings make the output a different recipe.
			return ErrInvalidInput
		}
	}
	if settings["-buildmode"] != "exe" || settings["-compiler"] != "gc" ||
		settings["-trimpath"] != "true" || settings["CGO_ENABLED"] != "0" ||
		settings["GOOS"] != goos || settings["GOARCH"] != goarch {
		return ErrInvalidInput
	}
	if goarch == "amd64" {
		if settings["GOAMD64"] != GOAMD64 {
			return ErrInvalidInput
		}
		if _, present := settings["GOARM64"]; present {
			return ErrInvalidInput
		}
	} else {
		if settings["GOARM64"] != GOARM64 {
			return ErrInvalidInput
		}
		if _, present := settings["GOAMD64"]; present {
			return ErrInvalidInput
		}
	}
	if !helper && !bytes.Contains(content, []byte(Version)) {
		return ErrInvalidInput
	}
	if !strippedExecutable(content, goos, goarch) {
		return ErrInvalidInput
	}
	return nil
}

func splitBuildTarget(target string) (string, string, bool) {
	for _, expected := range targetOrder {
		if target == expected {
			parts := strings.SplitN(target, "-", 2)
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

func strippedExecutable(content []byte, goos, goarch string) bool {
	reader := bytes.NewReader(content)
	switch goos {
	case "linux":
		file, err := elf.NewFile(reader)
		if err != nil || (goarch == "amd64" && file.Machine != elf.EM_X86_64) ||
			(goarch == "arm64" && file.Machine != elf.EM_AARCH64) || file.Section(".symtab") != nil {
			return false
		}
		for _, section := range file.Sections {
			if strings.HasPrefix(section.Name, ".debug_") || strings.HasPrefix(section.Name, ".zdebug_") {
				return false
			}
		}
		return true
	case "darwin":
		file, err := macho.NewFile(reader)
		if err != nil || (goarch == "amd64" && file.Cpu != macho.CpuAmd64) ||
			(goarch == "arm64" && file.Cpu != macho.CpuArm64) || !onlyMachOUndefinedImports(file.Symtab) {
			return false
		}
		for _, section := range file.Sections {
			if section.Seg == "__DWARF" || strings.HasPrefix(section.Name, "__debug_") {
				return false
			}
		}
		return true
	case "windows":
		file, err := pe.NewFile(reader)
		if err != nil || (goarch == "amd64" && file.Machine != pe.IMAGE_FILE_MACHINE_AMD64) ||
			(goarch == "arm64" && file.Machine != pe.IMAGE_FILE_MACHINE_ARM64) ||
			len(file.Symbols) != 0 || len(file.COFFSymbols) != 0 {
			return false
		}
		for _, section := range file.Sections {
			if strings.HasPrefix(section.Name, ".debug") {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func onlyMachOUndefinedImports(table *macho.Symtab) bool {
	if table == nil {
		return true
	}
	// A fully stripped Mach-O still needs undefined external symbols for its
	// dynamic imports. Reject any defined, local, stab, or private symbol.
	const (
		stabMask = uint8(0xe0)
		typeMask = uint8(0x0e)
		external = uint8(0x01)
	)
	for _, symbol := range table.Syms {
		if symbol.Type&stabMask != 0 || symbol.Type&typeMask != 0 || symbol.Type&external == 0 || symbol.Sect != 0 || symbol.Value != 0 {
			return false
		}
	}
	return true
}
