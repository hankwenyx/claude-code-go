package api

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt(t *testing.T) {
	tests := []struct {
		name         string
		opts         BuildOptions
		wantBlocks   int
		wantContains []string
	}{
		{
			name: "minimal options",
			opts: BuildOptions{
				CWD: "/test/path",
			},
			wantBlocks: 2,
			wantContains: []string{
				"gocc",
				"Current working directory: /test/path",
			},
		},
		{
			name: "with enabled tools",
			opts: BuildOptions{
				CWD:          "/test/path",
				EnabledTools: []string{"Bash", "Read", "Write"},
			},
			wantBlocks: 2,
			wantContains: []string{
				"Available tools: Bash, Read, Write",
			},
		},
		{
			name: "with claude md content",
			opts: BuildOptions{
				CWD:             "/test/path",
				ClaudeMdContent: "Project specific instructions",
			},
			wantBlocks: 2,
			wantContains: []string{
				"Project specific instructions",
			},
		},
		{
			name: "all options",
			opts: BuildOptions{
				CWD:             "/test/path",
				ClaudeMdContent: "# Project Rules\nAlways test code",
				EnabledTools:    []string{"Bash", "Glob"},
			},
			wantBlocks: 2,
			wantContains: []string{
				"gocc",
				"Available tools: Bash, Glob",
				"Current working directory: /test/path",
				"# Project Rules",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := BuildSystemPrompt(tt.opts)
			if len(blocks) != tt.wantBlocks {
				t.Errorf("BuildSystemPrompt() got %d blocks, want %d", len(blocks), tt.wantBlocks)
			}

			// Check first block has cache control
			if len(blocks) > 0 && blocks[0].CacheControl == nil {
				t.Error("first block should have cache_control")
			}

			// Check content contains expected strings
			for _, want := range tt.wantContains {
				found := false
				for _, block := range blocks {
					if strings.Contains(block.Text, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("BuildSystemPrompt() missing expected content: %q", want)
				}
			}
		})
	}
}

func TestBuildStaticBlock(t *testing.T) {
	tests := []struct {
		name         string
		enabledTools []string
		wantContains []string
		dontWant     []string
	}{
		{
			name:         "no tools",
			enabledTools: nil,
			wantContains: []string{
				"gocc",
				"Core Principles",
				"Tool Usage",
			},
			dontWant: []string{"Available tools:"},
		},
		{
			name:         "with tools",
			enabledTools: []string{"Bash", "Read", "Write"},
			wantContains: []string{
				"Available tools: Bash, Read, Write",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildStaticBlock(tt.enabledTools)

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("buildStaticBlock() missing %q", want)
				}
			}

			for _, dont := range tt.dontWant {
				if strings.Contains(result, dont) {
					t.Errorf("buildStaticBlock() should not contain %q", dont)
				}
			}
		})
	}
}

func TestBuildDynamicBlock(t *testing.T) {
	tests := []struct {
		name         string
		opts         BuildOptions
		wantContains []string
	}{
		{
			name: "with cwd only",
			opts: BuildOptions{
				CWD: "/home/user/project",
			},
			wantContains: []string{
				"Current working directory: /home/user/project",
				"Today's date is",
			},
		},
		{
			name: "with claude md content",
			opts: BuildOptions{
				CWD:             "/project",
				ClaudeMdContent: "Always use tabs for indentation",
			},
			wantContains: []string{
				"Always use tabs for indentation",
			},
		},
		{
			name: "empty options",
			opts: BuildOptions{},
			wantContains: []string{
				"Today's date is",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDynamicBlock(tt.opts)

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("buildDynamicBlock() missing %q", want)
				}
			}
		})
	}
}
