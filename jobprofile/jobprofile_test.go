package jobprofile

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		profile JobProfile
		wantErr string // substring the error must contain; "" means no error
	}{
		{
			name: "well-formed profile passes",
			profile: JobProfile{
				Name: "coder",
				Tier: TierMid,
				Tools: []SurfaceScope{
					{Surface: "fs", Actions: []string{"read", "write"}},
					{Surface: "http"},
				},
			},
			wantErr: "",
		},
		{
			name:    "empty name rejected",
			profile: JobProfile{Name: "  ", Tier: TierLocal},
			wantErr: "has no name",
		},
		{
			name:    "missing tier rejected",
			profile: JobProfile{Name: "coder"},
			wantErr: "has no tier",
		},
		{
			name:    "unknown tier rejected",
			profile: JobProfile{Name: "coder", Tier: Tier("frontier")},
			wantErr: "unknown tier",
		},
		{
			name: "empty surface rejected",
			profile: JobProfile{
				Name:  "coder",
				Tier:  TierStrong,
				Tools: []SurfaceScope{{Surface: "  "}},
			},
			wantErr: "no surface",
		},
		{
			name: "duplicate surface rejected",
			profile: JobProfile{
				Name: "coder",
				Tier: TierLocal,
				Tools: []SurfaceScope{
					{Surface: "fs"},
					{Surface: "fs", Actions: []string{"read"}},
				},
			},
			wantErr: "more than once",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.profile.Validate()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Validate() = %q, want it to contain %q", err.Error(), c.wantErr)
			}
		})
	}
}

func TestValidateTiersAllAccepted(t *testing.T) {
	for _, tier := range []Tier{TierLocal, TierMid, TierStrong} {
		p := JobProfile{Name: "p", Tier: tier}
		if err := p.Validate(); err != nil {
			t.Errorf("tier %q rejected: %v", tier, err)
		}
	}
}

func TestSurfacesSortedAndComplete(t *testing.T) {
	p := JobProfile{
		Name: "coder",
		Tier: TierMid,
		Tools: []SurfaceScope{
			{Surface: "http"},
			{Surface: "fs"},
			{Surface: "shell"},
		},
	}
	got := p.Surfaces()
	want := []string{"fs", "http", "shell"}
	if len(got) != len(want) {
		t.Fatalf("Surfaces() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Surfaces()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSurfacesEmpty(t *testing.T) {
	p := JobProfile{Name: "reader", Tier: TierLocal}
	if got := p.Surfaces(); len(got) != 0 {
		t.Errorf("Surfaces() = %v, want empty", got)
	}
}
