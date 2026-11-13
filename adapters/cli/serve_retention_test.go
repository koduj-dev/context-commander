package cli

import "testing"

// The flag wins over the environment, the environment takes a bare number
// or one with a "d" suffix, and anything that is not a number of days stops
// startup instead of being read as "keep forever" or "delete everything".
func TestUsageRetentionDays(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		env     string
		want    int
		wantErr bool
	}{
		{name: "default keeps forever", want: 0},
		{name: "flag", args: []string{"--usage-ledger-retention=90"}, want: 90},
		{name: "env", env: "30", want: 30},
		{name: "env with a day suffix", env: "400d", want: 400},
		{name: "flag beats env", args: []string{"--usage-ledger-retention=7"}, env: "30", want: 7},
		{name: "flag 0 beats env", args: []string{"--usage-ledger-retention=0"}, env: "30", want: 0},
		{name: "env not a number", env: "a year", wantErr: true},
		{name: "env negative", env: "-1", wantErr: true},
		{name: "flag negative", args: []string{"--usage-ledger-retention=-5"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(usageRetentionEnv, c.env)
			cmd := newServeCmd()
			if err := cmd.ParseFlags(c.args); err != nil {
				t.Fatal(err)
			}
			got, err := usageRetentionDays(cmd)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error %v", err, c.wantErr)
			}
			if !c.wantErr && got != c.want {
				t.Errorf("days = %d, want %d", got, c.want)
			}
		})
	}
}
