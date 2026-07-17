package core

import "testing"

func recipeTestRegistry() *Registry {
	reg := NewRegistry()
	for _, id := range []string{"workspace.switch", "app.open", "omarchy.run"} {
		_ = reg.Register(fakeCap{id: id})
	}
	return reg
}

func TestRecipeValidate(t *testing.T) {
	reg := recipeTestRegistry()
	valid := Recipe{
		Name:  "work",
		Steps: []Step{{Capability: "workspace.switch", Args: Args{"workspace": "2"}}},
	}

	tests := []struct {
		name    string
		recipe  Recipe
		wantErr bool
	}{
		{"valid recipe", valid, false},
		{"no name", Recipe{Steps: valid.Steps}, true},
		{"blank name", Recipe{Name: "   ", Steps: valid.Steps}, true},
		{"no steps", Recipe{Name: "empty"}, true},
		{"unknown capability", Recipe{Name: "bad", Steps: []Step{{Capability: "does.not.exist"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.recipe.Validate(reg)
			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRecipeLifecycleGuard(t *testing.T) {
	tests := []struct {
		name    string
		args    Args
		refused bool
	}{
		{"benign app launch", Args{"cmd": "firefox"}, false},
		{"benign omarchy subcommand", Args{"args": "restart waybar"}, false},
		{"benign workspace number", Args{"workspace": "3"}, false},
		{"pkill the agent", Args{"cmd": "pkill hyprvalet"}, true},
		{"killall", Args{"cmd": "killall Hyprland"}, true},
		{"reboot", Args{"args": "reboot"}, true},
		{"systemctl", Args{"args": "systemctl restart display-manager"}, true},
		{"self reference by name", Args{"cmd": "hyprvalet list"}, true},
		{"compositor exit", Args{"cmd": "hyprctl dispatch exit"}, true},
		{"kill is a whole word, not a substring of skill", Args{"cmd": "skillshare"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Recipe{Name: "t", Steps: []Step{{Capability: "app.open", Args: tt.args}}}
			err := r.guardLifecycle()
			if tt.refused && err == nil {
				t.Fatalf("guard should have refused %v", tt.args)
			}
			if !tt.refused && err != nil {
				t.Fatalf("guard wrongly refused %v: %v", tt.args, err)
			}
		})
	}
}

func TestRecipeBook(t *testing.T) {
	reg := recipeTestRegistry()
	book := NewRecipeBook()
	mk := func(name string) Recipe {
		return Recipe{Name: name, Steps: []Step{{Capability: "workspace.switch", Args: Args{"workspace": "1"}}}}
	}

	t.Run("adds and gets a recipe", func(t *testing.T) {
		if err := book.Add(mk("work"), reg); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if _, ok := book.Get("work"); !ok {
			t.Fatal("Get did not find an added recipe")
		}
	})

	t.Run("rejects a duplicate name", func(t *testing.T) {
		if err := book.Add(mk("work"), reg); err == nil {
			t.Fatal("Add accepted a duplicate recipe name")
		}
	})

	t.Run("rejects an invalid recipe", func(t *testing.T) {
		if err := book.Add(Recipe{Name: "broken"}, reg); err == nil {
			t.Fatal("Add accepted a recipe with no steps")
		}
	})

	t.Run("lists sorted by name", func(t *testing.T) {
		b := NewRecipeBook()
		for _, n := range []string{"charlie", "alpha", "bravo"} {
			if err := b.Add(mk(n), reg); err != nil {
				t.Fatalf("setup: %v", err)
			}
		}
		got := b.List()
		want := []string{"alpha", "bravo", "charlie"}
		for i, r := range got {
			if r.Name != want[i] {
				t.Fatalf("List[%d] = %q, want %q", i, r.Name, want[i])
			}
		}
	})
}
