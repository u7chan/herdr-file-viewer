package filesystem

import (
	"reflect"
	"testing"
)

func TestParseGitStatusPorcelain(t *testing.T) {
	output := []byte(" M modified\x00A  added\x00?? untracked-dir/\x00UU unmerged\x00 D deleted\x00!! ignored\x00R  renamed-new\x00renamed-old\x00")
	want := []GitStatusEntry{
		{Path: "modified", Status: GitStatusModified},
		{Path: "added", Status: GitStatusAdded},
		{Path: "untracked-dir", Status: GitStatusUntracked},
		{Path: "unmerged", Status: GitStatusUnmerged},
		{Path: "deleted", Status: GitStatusDeleted},
		{Path: "renamed-new", Status: GitStatusModified},
	}

	if got := parseGitStatusPorcelain(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("parseGitStatusPorcelain() = %#v, want %#v", got, want)
	}
}

func TestGitStatusForXYRecognizesCombinedStatuses(t *testing.T) {
	tests := []struct {
		name       string
		index      byte
		worktree   byte
		wantStatus GitStatus
	}{
		{name: "conflicting delete", index: 'D', worktree: 'D', wantStatus: GitStatusUnmerged},
		{name: "conflicting add", index: 'A', worktree: 'A', wantStatus: GitStatusUnmerged},
		{name: "staged delete", index: 'D', worktree: ' ', wantStatus: GitStatusDeleted},
		{name: "unstaged add", index: ' ', worktree: 'A', wantStatus: GitStatusAdded},
		{name: "worktree modification", index: ' ', worktree: 'M', wantStatus: GitStatusModified},
		{name: "untracked", index: '?', worktree: '?', wantStatus: GitStatusUntracked},
		{name: "ignored", index: '!', worktree: '!', wantStatus: GitStatusNone},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gitStatusForXY(test.index, test.worktree); got != test.wantStatus {
				t.Fatalf("gitStatusForXY(%q, %q) = %v, want %v", test.index, test.worktree, got, test.wantStatus)
			}
		})
	}
}

func TestParseGitIgnoreOutput(t *testing.T) {
	tests := []struct {
		name   string
		output []byte
		want   []string
	}{
		{name: "empty output", output: nil, want: nil},
		{name: "trailing nul", output: []byte("a.txt\x00dir/\x00"), want: []string{"a.txt", "dir/"}},
		{name: "missing trailing nul", output: []byte("a.txt\x00dir/\x00b.log"), want: []string{"a.txt", "dir/", "b.log"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseGitIgnoreOutput(test.output); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseGitIgnoreOutput(%q) = %#v, want %#v", test.output, got, test.want)
			}
		})
	}
}
