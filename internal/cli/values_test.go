package cli

import "testing"

func TestFlagPresenceIncludesEmptyAndFalseValues(t *testing.T) {
	cmd := &Command{Name: "test", Flags: []Flag{
		{Name: "model", Kind: KindOptString},
		{Name: "set", Kind: KindStrings},
	}}
	for _, args := range [][]string{{"--model"}, {"--model="}, {"--json=false"}, {"--set="}} {
		c, err := parse(cmd, args)
		if err != nil {
			t.Fatal(err)
		}
		name, _, _ := splitFlag(args[0])
		if !c.Flags.Present(name) {
			t.Fatalf("%v was treated as absent", args)
		}
		if c.Flags.Present("host") {
			t.Fatal("absent flag was treated as present")
		}
	}
	c, err := parse(cmd, []string{"--model", "exec"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Flags.Detach("model"); got != "exec" || !c.Flags.Present("model") || c.Flags.String("model") != "" {
		t.Fatal("returning a detached argument must leave a bare model flag")
	}
}
