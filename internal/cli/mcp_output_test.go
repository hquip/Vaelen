package cli

import (
	"strings"
	"testing"
	"unicode/utf8"

	"vsm/internal/core"
)

func TestMCPTaskOutputBudgetPreservesUTF8(t *testing.T) {
	// A 64 KiB task output ending inside a three-byte rune becomes 65,538
	// bytes when RunTasks replaces its incomplete final rune with U+FFFD.
	output := strings.Repeat("界", 21845) + "�"
	report := core.TaskReport{}
	for range 17 {
		report.Tasks = append(report.Tasks, core.TaskResult{Output: output})
	}
	const limit = 1 << 20
	capMCPTaskOutput(&report, limit)
	total := 0
	for i, task := range report.Tasks {
		total += len(task.Output)
		if !utf8.ValidString(task.Output) {
			t.Fatalf("task %d output is not valid UTF-8", i)
		}
		if i < 15 && (task.Output != output || task.Truncated) {
			t.Fatalf("task %d changed before the budget was exhausted", i)
		}
		if i >= 15 && !task.Truncated {
			t.Fatalf("task %d lost its truncation flag", i)
		}
	}
	if total > limit || total != limit-1 {
		t.Fatalf("output used %d bytes of a %d-byte budget", total, limit)
	}
}

func TestMCPTaskOutputBudgetKeepsUntruncatedOutput(t *testing.T) {
	for _, output := range []string{"ASCII", "界😀", "", "�"} {
		report := core.TaskReport{Tasks: []core.TaskResult{{Output: output, Truncated: true}}}
		capMCPTaskOutput(&report, len(output))
		if report.Tasks[0].Output != output || !report.Tasks[0].Truncated {
			t.Fatalf("output or existing truncation flag changed: %+v", report.Tasks[0])
		}
	}
}

func TestMCPTaskOutputZeroBudget(t *testing.T) {
	for _, budget := range []int{0, -1} {
		report := core.TaskReport{Tasks: []core.TaskResult{{Output: "界"}, {Output: "ASCII"}, {}}}
		capMCPTaskOutput(&report, budget)
		if report.Tasks[0].Output != "" || !report.Tasks[0].Truncated || report.Tasks[1].Output != "" || !report.Tasks[1].Truncated || report.Tasks[2].Truncated {
			t.Fatalf("unexpected output with budget %d: %+v", budget, report.Tasks)
		}
	}
}
