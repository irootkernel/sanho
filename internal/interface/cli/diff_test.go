package cli

import "testing"

func TestDiffOptionsRejectAmbiguousModes(t *testing.T) {
	tests := []diffOptions{
		{stat: true, nameOnly: true},
		{local: true, refresh: true},
	}
	for _, opts := range tests {
		if err := opts.validate(); err == nil {
			t.Errorf("validate(%+v) succeeded, want an error", opts)
		}
	}
}

func TestDiffOptionsAcceptSupportedModes(t *testing.T) {
	tests := []diffOptions{{}, {refresh: true}, {local: true}, {stat: true}, {nameOnly: true}}
	for _, opts := range tests {
		if err := opts.validate(); err != nil {
			t.Errorf("validate(%+v) error = %v", opts, err)
		}
	}
}
