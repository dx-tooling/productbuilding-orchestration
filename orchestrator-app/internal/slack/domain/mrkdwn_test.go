package domain

import "testing"

func TestMarkdownToMrkdwn(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Cycle 1: Bold
		{"double asterisks to single", "**Kurz zusammengefasst:**", "*Kurz zusammengefasst:*"},
		{"multiple bold spans", "Von **26 auf 81 Dienstleister** und **7 Städte**", "Von *26 auf 81 Dienstleister* und *7 Städte*"},

		// Cycle 2: Headings
		{"h3 heading to bold", "### Zusammenfassung\nContent here", "*Zusammenfassung*\nContent here"},
		{"h2 heading", "## Status", "*Status*"},

		// Cycle 3: Idempotency
		{"already single asterisks", "*already bold*", "*already bold*"},
		{"backticks unchanged", "`code here`", "`code here`"},
		{"plain text unchanged", "no formatting here", "no formatting here"},
		{"slack links unchanged", "<https://example.com|link>", "<https://example.com|link>"},

		// Cycle 4: Real-world regression test
		{"real world example",
			"**Kurz zusammengefasst:**\n- Von **26 auf 81 Dienstleister** aufgestockt\n- Alle **7 leeren Städte** haben jetzt Einträge\n- Alle **Kategorien** sind mit mindestens 3–4 Einträgen vertreten",
			"*Kurz zusammengefasst:*\n- Von *26 auf 81 Dienstleister* aufgestockt\n- Alle *7 leeren Städte* haben jetzt Einträge\n- Alle *Kategorien* sind mit mindestens 3–4 Einträgen vertreten"},

		// Cycle 5: Edge cases
		{"empty string", "", ""},
		{"triple asterisks", "***important***", "*important*"},
		{"heading with bold text", "### **Status Update**\nDetails", "*Status Update*\nDetails"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToMrkdwn(tt.input)
			if got != tt.want {
				t.Errorf("MarkdownToMrkdwn(%q)\n got = %q\nwant = %q", tt.input, got, tt.want)
			}
		})
	}
}
