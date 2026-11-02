package search

import "testing"

func TestCleanDescription(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"leading", ":chart_with_upwards_trend: Metrics library", "Metrics library"},
		{"multiple", "  :rocket: Fast  :sparkles: HTTP client :+1: ", "Fast HTTP client"},
		{"adjacent words", "HTTP:rocket:client", "HTTP client"},
		{"adjacent codes", ":rocket::smile: Client", "Client"},
		{"unknown", ":custom_emoji: Client :rocket:", ":custom_emoji: Client"},
		{"code only", ":rocket: :sparkles:", ""},
		{"unicode", "\U0001F680 Client :rocket:", "\U0001F680 Client"},
		{"ordinary colons", "HTTP: https://localhost:8080 std::vector :placeholder:", "HTTP: https://localhost:8080 std::vector :placeholder:"},
		{"unchanged whitespace", "Plain  text\nwith spacing", "Plain  text\nwith spacing"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanDescription(tc.input); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
