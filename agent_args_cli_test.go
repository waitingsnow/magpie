package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/provider"
)

// cliHome is groupsHome with every other folder an agent keeps files in
// (XDG_DATA_HOME, XDG_STATE_HOME, Windows' APPDATA) inside its home, so
// that homeFiles sees whatever a command writes, and with the gateway's
// address one nothing answers at.
func cliHome(t *testing.T) {
	t.Helper()
	groupsHome(t)
	home := os.Getenv("HOME")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("MAGPIE_ADDR", "127.0.0.1:1")
}

// noBrowser puts failing stand-ins for the browser openers first on PATH:
// Cindy's link opened in the browser for any word after magpie cindy.
func noBrowser(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"open", "xdg-open", "rundll32"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "windows" {
			if err := os.WriteFile(filepath.Join(bin, name+".bat"), []byte("@exit /b 1\r\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// homeFiles is every folder and file in the test's home, config and cache
// folders, with each file's contents. Not magpie's cli-identity.json: who
// Cursor's and Devin's CLIs say is signed in, asked again behind a look a
// minute after the last answer and kept when it comes, which may be during
// a later command, or in a later test's home.
func homeFiles(t *testing.T) map[string]string {
	t.Helper()
	identities := filepath.Join(filepath.Dir(provider.Path()), "cli-identity.json")
	m := map[string]string{}
	for _, root := range []string{os.Getenv("HOME"), os.Getenv("XDG_CONFIG_HOME"), os.Getenv("XDG_CACHE_HOME")} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == identities {
				return nil
			}
			if d.IsDir() {
				m[p] = "(folder)"
				return nil
			}
			b, err := os.ReadFile(p)
			m[p] = string(b)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return m
}

// filesChanged names what differs between two homeFiles, "" for nothing.
func filesChanged(before, after map[string]string) string {
	var out []string
	for p, b := range before {
		if a, ok := after[p]; !ok {
			out = append(out, "removed "+p)
		} else if a != b {
			out = append(out, "changed "+p+":\n"+a)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			out = append(out, "made "+p)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func writeHomeFile(t *testing.T, rel, s string) string {
	t.Helper()
	p := filepath.Join(os.Getenv("HOME"), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// codexOnAdvanced is the reporter's machine: Codex's own config.toml, a
// routing group advanced, and Codex put on it with magpie codex
// group/advanced. It gives the config's path.
func codexOnAdvanced(t *testing.T) string {
	t.Helper()
	cliHome(t)
	path := writeHomeFile(t, filepath.Join(".codex", "config.toml"),
		"model = \"gpt-5.5\"\nmodel_reasoning_effort = \"high\"\n\n[projects.\"/Users/me/work\"]\ntrust_level = \"trusted\"\n")
	for _, args := range [][]string{{"group", "add", "advanced", "models=a/m,b/gpt-5.5"}, {"codex", "group/advanced"}} {
		if _, err := printed(t, func() error { return run(args) }); err != nil {
			t.Fatalf("magpie %s: %v", strings.Join(args, " "), err)
		}
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"group/advanced"`) || !strings.Contains(string(b), "trust_level") {
		t.Fatalf("Codex isn't on group/advanced:\n%s", b)
	}
	return path
}

// magpie codex --help printed "✓ Codex model --help" and wrote model =
// "--help" into ~/.codex/config.toml, magpie taken out with it (reported
// on 0.1.1082, Codex on group/advanced). --help, -h and help, wherever
// they come, now show how to set Codex, and any other word that starts
// with "-" is refused as a flag: neither writes a thing.
func TestAgentHelpWritesNothing(t *testing.T) {
	codexOnAdvanced(t)
	before := homeFiles(t)
	for _, args := range [][]string{{"codex", "--help"}, {"codex", "-h"}, {"codex", "help"}, {"codex", "Help"}, {"codex", "HELP"},
		{"codex", "model", "--help"}, {"codex", "effort", "-h"}, {"codex", "effort", "Help"}} {
		out, err := printed(t, func() error { return run(args) })
		if err != nil || !strings.HasPrefix(out, "usage:\n  magpie codex ") || !strings.Contains(out, "magpie codex <field> <value>") ||
			!strings.Contains(out, "fields: model, effort,") {
			t.Errorf("magpie %s: %v\n%s\nwant Codex's usage", strings.Join(args, " "), err, out)
		}
		if diff := filesChanged(before, homeFiles(t)); diff != "" {
			t.Fatalf("magpie %s wrote:\n%s", strings.Join(args, " "), diff)
		}
	}
	for _, args := range [][]string{{"codex", "-x"}, {"codex", "--model"}, {"codex", "-"}, {"codex", "effort", "-x"}} {
		flag := args[len(args)-1]
		out, err := printed(t, func() error { return run(args) })
		if err == nil || err.Error() != "unknown flag "+flag+" (magpie codex help)" || out != "" {
			t.Errorf("magpie %s: %v %q, want the flag refused", strings.Join(args, " "), err, out)
		}
		if diff := filesChanged(before, homeFiles(t)); diff != "" {
			t.Fatalf("magpie %s wrote:\n%s", strings.Join(args, " "), diff)
		}
	}
	// what tells a write: a value still goes in
	if _, err := printed(t, func() error { return run([]string{"codex", "effort", "low"}) }); err != nil {
		t.Fatal(err)
	}
	if filesChanged(before, homeFiles(t)) == "" {
		t.Fatal("magpie codex effort low wrote nothing")
	}
}

// Every agent takes its help and flags the same way, through any of its
// fields: they wrote "--help" as the model of most of them, turned magpie
// on for Claude Desktop, Cursor Private Inference, Pencil, T3 Code,
// WorkBuddy and ZCode, and opened Cindy's link in the browser. The usage
// names each field as it is typed and as it is shown (magpie gemini auth
// api-key sets its provider).
func TestEveryAgentHelpWritesNothing(t *testing.T) {
	cliHome(t)
	noBrowser(t)
	writeHomeFile(t, filepath.Join(".claude", "settings.json"), "{\n  \"model\": \"opus\",\n  \"theme\": \"dark\"\n}\n")
	if _, err := printed(t, func() error { return run([]string{"version"}) }); err != nil {
		t.Fatal(err)
	}
	before := homeFiles(t)
	for _, a := range agent.All() {
		cases := [][]string{{a.ID, "--help"}, {a.ID, "-h"}, {a.ID, "help"}, {a.ID, "-x"}}
		for _, f := range a.Fields {
			cases = append(cases, []string{a.ID, f.Key, "--help"}, []string{a.ID, f.Key, "-x"})
		}
		for _, args := range cases {
			out, err := printed(t, func() error { return run(args) })
			if args[len(args)-1] == "-x" {
				if err == nil || !strings.HasPrefix(err.Error(), "unknown flag -x") {
					t.Errorf("magpie %s: %v %q, want the flag refused", strings.Join(args, " "), err, out)
				}
			} else if err != nil || !strings.HasPrefix(out, "usage:\n  magpie "+a.ID+" ") {
				t.Errorf("magpie %s: %v %q, want %s's usage", strings.Join(args, " "), err, out, a.Name)
			} else {
				for _, f := range a.Fields {
					name := f.Key
					if f.Label != f.Key {
						name += " (" + f.Label + ")"
					}
					if !strings.Contains(out, " "+name) {
						t.Errorf("magpie %s doesn't name %s:\n%s", strings.Join(args, " "), name, out)
					}
				}
				if a.Import != nil && !strings.Contains(out, "  magpie "+a.ID+" add  ") {
					t.Errorf("magpie %s doesn't say how to add magpie:\n%s", strings.Join(args, " "), out)
				}
			}
			if diff := filesChanged(before, homeFiles(t)); diff != "" {
				t.Errorf("magpie %s wrote:\n%s", strings.Join(args, " "), diff)
				before = homeFiles(t)
			}
		}
	}
}
