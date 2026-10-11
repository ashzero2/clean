package catalog

// Defaults is the built-in category catalog. Paths may use ~ and ${VAR}; they are
// expanded by the scope resolver.
func Defaults() []Category {
	cats := []Category{
		{ID: "rust-target", Label: "Rust target/", Tier: Rebuildable, Kind: KindDiscover, Match: []string{"target"}, CleanHint: "cargo clean", Doc: "Cargo build output."},
		{ID: "node-modules", Label: "node_modules", Tier: Rebuildable, Kind: KindDiscover, Match: []string{"node_modules"}, CleanHint: "npm install", Doc: "Installed Node dependencies."},
		{ID: "python-venv", Label: "Python venvs", Tier: Rebuildable, Kind: KindDiscover, Match: []string{".venv", "venv"}, Doc: "Virtual environments."},
		{ID: "cargo-registry", Label: "Cargo registry", Tier: Safe, Kind: KindPath, Paths: []string{"~/.cargo/registry", "~/.cargo/git"}, CleanHint: "cargo cache -a"},
		{ID: "go-build", Label: "Go build cache", Tier: Safe, Kind: KindPath, Paths: []string{"~/Library/Caches/go-build"}, CleanHint: "go clean -cache"},
		{ID: "go-mod", Label: "Go module cache", Tier: Safe, Kind: KindPath, Paths: []string{"~/go/pkg/mod"}, CleanHint: "go clean -modcache"},
		{ID: "npm", Label: "npm cache", Tier: Safe, Kind: KindPath, Paths: []string{"~/.npm"}},
		{ID: "pnpm", Label: "pnpm store", Tier: Safe, Kind: KindPath, Paths: []string{"~/.local/share/pnpm/store"}},
		{ID: "yarn", Label: "Yarn cache", Tier: Safe, Kind: KindPath, Paths: []string{"~/Library/Caches/Yarn"}},
		{ID: "pip", Label: "pip cache", Tier: Safe, Kind: KindPath, Paths: []string{"~/Library/Caches/pip"}},
		{ID: "uv", Label: "uv cache", Tier: Safe, Kind: KindPath, Paths: []string{"~/.cache/uv"}},
		{ID: "maven", Label: "Maven .m2", Tier: Safe, Kind: KindPath, Paths: []string{"~/.m2/repository"}},
		{ID: "gradle", Label: "Gradle caches", Tier: Safe, Kind: KindPath, Paths: []string{"~/.gradle/caches"}},
		{ID: "homebrew", Label: "Homebrew cache", Tier: Safe, Kind: KindPath, Paths: []string{"~/Library/Caches/Homebrew"}, CleanHint: "brew cleanup"},
		{ID: "cocoapods", Label: "CocoaPods cache", Tier: Safe, Kind: KindPath, Paths: []string{"~/Library/Caches/CocoaPods"}},
		{ID: "xcode-deriveddata", Label: "Xcode DerivedData", Tier: Safe, Kind: KindPath, Paths: []string{"~/Library/Developer/Xcode/DerivedData"}},
		{ID: "trash", Label: "Trash", Tier: Safe, Kind: KindPath, Paths: []string{"~/.Trash"}},
		{ID: "xcode-archives", Label: "Xcode Archives", Tier: Review, Kind: KindPath, Paths: []string{"~/Library/Developer/Xcode/Archives"}},
		{ID: "xcode-devicesupport", Label: "Xcode DeviceSupport", Tier: Review, Kind: KindPath, Paths: []string{"~/Library/Developer/Xcode/iOS DeviceSupport"}},
		{ID: "coresimulator", Label: "iOS Simulators", Tier: Review, Kind: KindPath, Paths: []string{"~/Library/Developer/CoreSimulator"}},
		{ID: "docker", Label: "Docker data", Tier: Review, Kind: KindPath, Paths: []string{"~/Library/Containers/com.docker.docker"}},
		{ID: "ollama", Label: "Ollama models", Tier: Review, Kind: KindPath, Paths: []string{"~/.ollama/models"}},
		{ID: "huggingface", Label: "Hugging Face cache", Tier: Review, Kind: KindPath, Paths: []string{"~/.cache/huggingface"}},
	}
	for _, name := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Music", "Movies", "Public"} {
		cats = append(cats, Category{
			ID:    "user-" + name,
			Label: name,
			Tier:  UserData,
			Kind:  KindUserFolder,
			Paths: []string{"~/" + name},
		})
	}
	return cats
}
