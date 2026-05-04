package checkpoint

import (
	"path/filepath"
	"sort"
	"testing"
)

// TestBashLikelyWritesFiles_PositiveCases: commands that mutate files must return true.
func TestBashLikelyWritesFiles_PositiveCases(t *testing.T) {
	cases := []string{
		`echo hello > out.txt`,
		`echo more >> out.txt`,
		`cat src.go > dst.go`,
		`mv old.go new.go`,
		`cp src.go dst.go`,
		`rm stale.txt`,
		`rm -rf build/`,
		`touch new.go`,
		`mkdir -p pkg/foo`,
		`rmdir empty`,
		`ln -s a b`,
		`chmod +x script.sh`,
		`chown user:group file`,
		`patch -p1 < change.patch`,
		`rsync -av src/ dst/`,
		`sed -i 's/foo/bar/g' main.go`,
		`sed -i.bak 's/foo/bar/' main.go`,
		`sed --in-place 's/x/y/' f.txt`,
		`awk -i inplace '{print}' f.txt`,
		`git checkout main`,
		`git restore --staged file.go`,
		`git reset --hard HEAD`,
		`git apply patch.diff`,
		`git pull origin main`,
		`git merge feature`,
		`git rebase main`,
		`git cherry-pick abc123`,
		`git revert HEAD`,
		`git commit -m "x"`,
		`git add .`,
		`git rm old.go`,
		`git mv a b`,
		`git stash`,
		`git clean -fd`,
		`npm install`,
		`npm i lodash`,
		`yarn add react`,
		`pnpm install`,
		`pnpm add foo`,
		`pip install requests`,
		`pip3 install -r requirements.txt`,
		`go get github.com/x/y`,
		`go install ./cmd/x`,
		`go mod tidy`,
		`go mod download`,
		`go mod vendor`,
		`go generate ./...`,
		`cargo install ripgrep`,
		`cargo build`,
		`cargo update`,
		`unzip archive.zip`,
		`tar -xzf bundle.tar.gz`,
		`tar xvf bundle.tar`,
		`gunzip data.gz`,
		`bunzip2 data.bz2`,
		`find . -name '*.tmp' -delete`,
		`find . -type f -exec rm {} \;`,
		`echo content | tee out.txt`,
		`dd if=/dev/zero of=disk.img bs=1M count=100`,
		`truncate -s 0 log.txt`,
	}
	for _, cmd := range cases {
		if !BashLikelyWritesFiles(cmd) {
			t.Errorf("expected write=true for %q", cmd)
		}
	}
}

// TestBashLikelyWritesFiles_NegativeCases: read-only commands must return false.
func TestBashLikelyWritesFiles_NegativeCases(t *testing.T) {
	cases := []string{
		``,
		`   `,
		`ls`,
		`ls -la`,
		`pwd`,
		`cat file.txt`,
		`cat file.txt 2>&1`,
		`head -20 file.go`,
		`tail -f log.txt`,
		`grep -r foo .`,
		`grep -n bar file.go`,
		`find . -name '*.go'`,
		`find . -type f`,
		`git status`,
		`git log --oneline`,
		`git diff main`,
		`git show HEAD`,
		`git branch`,
		`git remote -v`,
		`go version`,
		`go env`,
		`go test ./...`,
		`go vet ./...`,
		`go build ./...`,
		`echo hello`,
		`printf "x"`,
		`which bash`,
		`whoami`,
		`uname -a`,
		`ps aux`,
		`df -h`,
		`du -sh .`,
		`wc -l main.go`,
		`sort file.txt`,
		`uniq file.txt`,
		`diff a.txt b.txt`,
		`stat file.go`,
		`file binary`,
		`echo done 2>&1 1>&2`,
		// echo with stderr-only redirection should remain read-only after stripHarmlessRedirects
		`some_cmd >&2`,
	}
	for _, cmd := range cases {
		if BashLikelyWritesFiles(cmd) {
			t.Errorf("expected write=false for %q", cmd)
		}
	}
}

// TestSourceFilesInWorkdir_ExtractsFromCommonPatterns: paths inside workdir with recognized
// extensions are returned, paths outside or with unknown extensions are filtered.
func TestSourceFilesInWorkdir_ExtractsFromCommonPatterns(t *testing.T) {
	root := t.TempDir()

	// Helper to convert a workdir-relative slash path into the platform abs path.
	abs := func(rel string) string {
		return filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	}

	type tc struct {
		name    string
		command string
		want    []string
	}

	cases := []tc{
		{
			name:    "simple_relative_paths",
			command: `mv pkg/a.go pkg/b.go`,
			want:    []string{abs("pkg/a.go"), abs("pkg/b.go")},
		},
		{
			name:    "absolute_paths_inside_workdir",
			command: `cp ` + abs("src/main.go") + ` ` + abs("src/main_copy.go"),
			want:    []string{abs("src/main.go"), abs("src/main_copy.go")},
		},
		{
			name:    "redirect_target",
			command: `echo hello > notes.md`,
			want:    []string{abs("notes.md")},
		},
		{
			name:    "deduplicates_repeated_paths",
			command: `cp main.go main.go`,
			want:    []string{abs("main.go")},
		},
		{
			name:    "filters_unknown_extensions",
			command: `mv binary.bin other.exe`,
			want:    nil,
		},
		{
			name:    "filters_paths_outside_workdir",
			command: `cat ../escape.go ../../further.go > out.log`,
			want:    []string{abs("out.log")},
		},
		{
			name:    "ignores_glob_arguments",
			command: `rm *.go pkg/*.json`,
			want:    nil,
		},
		{
			name:    "extracts_yaml_and_toml_paths",
			command: `sed -i 's/x/y/g' config.yaml settings.toml`,
			want:    []string{abs("config.yaml"), abs("settings.toml")},
		},
		{
			name:    "empty_command_returns_nil",
			command: ``,
			want:    nil,
		},
		{
			name:    "whitespace_command_returns_nil",
			command: `   `,
			want:    nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SourceFilesInWorkdir(c.command, root)
			gotSorted := append([]string(nil), got...)
			sort.Strings(gotSorted)
			wantSorted := append([]string(nil), c.want...)
			sort.Strings(wantSorted)
			if !equalStringSlice(gotSorted, wantSorted) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

// TestSourceFilesInWorkdir_HandlesEmptyWorkdir: with empty workdir we cannot compute
// safe relative paths, so the function should return nil for relative inputs.
func TestSourceFilesInWorkdir_HandlesEmptyWorkdir(t *testing.T) {
	got := SourceFilesInWorkdir(`mv a.go b.go`, "")
	if got != nil {
		t.Fatalf("expected nil with empty workdir, got %v", got)
	}
}
