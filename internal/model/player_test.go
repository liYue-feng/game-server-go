package model

import "testing"

func TestPlayerEffectiveLevel(t *testing.T) {
	tests := []struct {
		name  string
		level int
		want  int
	}{
		{name: "zero defaults to one", level: 0, want: 1},
		{name: "negative defaults to one", level: -4, want: 1},
		{name: "positive level is preserved", level: 7, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			player := Player{Level: tt.level}
			if got := player.EffectiveLevel(); got != tt.want {
				t.Fatalf("EffectiveLevel() = %d, want %d", got, tt.want)
			}
		})
	}
}
