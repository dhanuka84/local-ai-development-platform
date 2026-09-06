package contracts

import "testing"

func TestVersionedContractsAndFixtures(t *testing.T) {
	if err := Check(); err != nil {
		t.Fatal(err)
	}
}
