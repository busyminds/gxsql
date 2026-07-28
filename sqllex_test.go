package gxsql

import (
	"errors"
	"testing"
)

func TestSQLLexSemanticsSharedBetweenScopeAndTrustedCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		suffix    string
		wantErr   bool
		wantSlots int
	}{
		{
			name:      "dollar quoted literal before slot",
			suffix:    "note = $$x$$ AND tenant_id = ?",
			wantSlots: 1,
		},
		{
			name:    "literal question mark in single quotes",
			suffix:  "note = 'what?'",
			wantErr: true,
		},
		{
			name:    "json key operator rejected",
			suffix:  "payload ? 'active'",
			wantErr: true,
		},
		{
			name:      "slot before unterminated dollar quote tail",
			suffix:    "tenant_id = ? AND note = $tag$foo",
			wantSlots: 1,
		},
		{
			name:    "unterminated dollar quote with literal question mark",
			suffix:  "note = $tag$foo ?",
			wantErr: true,
		},
		{
			name:      "nested block comment before slot",
			suffix:    "note = 'x' /* outer /* inner */ tail */ AND tenant_id = ?",
			wantSlots: 1,
		},
		{
			name:    "question mark in nested block comment",
			suffix:  "note = 'x' /* outer /* inner */ ? */ AND tenant_id = ?",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scopeSlots, scopeErr := scanNeutralSlots(tc.suffix)
			template := "SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND " + tc.suffix
			customArgs := make([]any, tc.wantSlots)
			trustedErr := preflightTrustedCount(template, customArgs)

			if tc.wantErr {
				if scopeErr == nil {
					t.Fatal("scope scan: expected error")
				}
				if trustedErr == nil {
					t.Fatal("trusted count: expected error")
				}
				if !errors.Is(scopeErr, ErrCategoryUnsupported) {
					t.Fatalf("scope category = %v, want unsupported", scopeErr)
				}
				if !errors.Is(trustedErr, ErrCategoryUnsupported) {
					t.Fatalf("trusted count category = %v, want unsupported", trustedErr)
				}
				return
			}

			if scopeErr != nil {
				t.Fatalf("scope scan: %v", scopeErr)
			}
			if trustedErr != nil {
				t.Fatalf("trusted count: %v", trustedErr)
			}
			if scopeSlots != tc.wantSlots {
				t.Fatalf("scope slots = %d, want %d", scopeSlots, tc.wantSlots)
			}
			scan, err := scanTrustedCountTemplate(template)
			if err != nil {
				t.Fatalf("trusted count scan: %v", err)
			}
			if scan.customSlotCount != tc.wantSlots {
				t.Fatalf("trusted custom slots = %d, want %d", scan.customSlotCount, tc.wantSlots)
			}
		})
	}
}
