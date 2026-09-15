package workflow

import "testing"

func TestFindRefs(t *testing.T) {
	t.Parallel()
	refs, err := FindRefs("{{ services.cloud }}/vms/{{ inputs.plan }}")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("len = %d", len(refs))
	}
	if refs[0].Namespace != NamespaceServices || refs[0].Name != "cloud" {
		t.Fatalf("ref0 = %#v", refs[0])
	}
	if refs[1].Namespace != NamespaceInputs || refs[1].Name != "plan" {
		t.Fatalf("ref1 = %#v", refs[1])
	}
}

func TestParseRefStepOutput(t *testing.T) {
	t.Parallel()
	ref, err := ParseRef("steps.create_network.output.id")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "create_network" {
		t.Fatalf("name = %q", ref.Name)
	}
	if len(ref.Path) != 2 || ref.Path[0] != "output" || ref.Path[1] != "id" {
		t.Fatalf("path = %#v", ref.Path)
	}
}

func TestParseRefInvalid(t *testing.T) {
	t.Parallel()
	if _, err := ParseRef("env.TOKEN"); err == nil {
		t.Fatal("expected unknown namespace")
	}
	if _, err := ParseRef("steps.create_network.status"); err == nil {
		t.Fatal("expected .output requirement")
	}
}
