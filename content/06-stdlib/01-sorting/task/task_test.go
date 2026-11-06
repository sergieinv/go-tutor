package task

import (
	"slices"
	"testing"
)

func TestLeaderboard(t *testing.T) {
	tests := []struct {
		name string
		in   []Player
		want []Player
	}{
		{
			name: "по очкам",
			in:   []Player{{"a", 10, 5}, {"b", 30, 5}, {"c", 20, 5}},
			want: []Player{{"b", 30, 5}, {"c", 20, 5}, {"a", 10, 5}},
		},
		{
			name: "равные очки — по времени",
			in:   []Player{{"a", 10, 90}, {"b", 10, 60}, {"c", 10, 75}},
			want: []Player{{"b", 10, 60}, {"c", 10, 75}, {"a", 10, 90}},
		},
		{
			name: "равные очки и время — по имени",
			in:   []Player{{"zoe", 5, 1}, {"amy", 5, 1}, {"max", 5, 1}},
			want: []Player{{"amy", 5, 1}, {"max", 5, 1}, {"zoe", 5, 1}},
		},
		{
			name: "один игрок",
			in:   []Player{{"solo", 1, 1}},
			want: []Player{{"solo", 1, 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Leaderboard(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Leaderboard(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
