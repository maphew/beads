//go:build linux

package procid

import "testing"

func TestParseStartTime(t *testing.T) {
	tests := []struct {
		name string
		stat string
		want string
	}{
		{
			name: "ordinary command",
			stat: "123 (sleep) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 987654 20",
			want: "987654",
		},
		{
			name: "spaces and parentheses in command",
			stat: "123 (a) b) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 42 20",
			want: "42",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStartTime(tt.stat)
			if err != nil {
				t.Fatalf("parseStartTime: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseStartTime = %q, want %q", got, tt.want)
			}
		})
	}
}
