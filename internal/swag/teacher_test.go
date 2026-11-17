package swag

import (
	"strings"
	"testing"
)

func TestClassroomCheck(t *testing.T) {
	long := strings.Repeat("é", 200) // 200 characters, 400 bytes: the browser's maxlength counts characters
	for _, c := range []struct {
		name string
		in   *Classroom
		ok   bool
	}{
		{"missing", nil, false},
		{"empty", &Classroom{}, false},
		{"no students", &Classroom{School: "School", Grades: "9", Students: 0}, false},
		{"blank grades", &Classroom{School: "School", Grades: " ", Students: 20}, false},
		{"too many students", &Classroom{School: "School", Grades: "9", Students: 100001}, false},
		{"school too long", &Classroom{School: long + "x", Grades: "9", Students: 20}, false},
		{"non-ASCII at the limit", &Classroom{School: long, Grades: strings.Repeat("–", 100), Students: 20}, true},
		{"ok", &Classroom{School: "Example School", Grades: "9–12", Students: 30}, true},
	} {
		if err := c.in.Check(); (err == nil) != c.ok || (err != nil && err != ErrClassroom) {
			t.Errorf("%s: Check() = %v", c.name, err)
		}
	}
}

func TestClassroomNote(t *testing.T) {
	c := &Classroom{School: " Example School ", Grades: " 9–12 ", Students: 30}
	if note := c.Note(" Class introduction. "); note != "School: Example School\nGrades / ages: 9–12\nStudents: 30\n\nClass introduction." {
		t.Fatalf("note = %q", note)
	}
	if !Purposes[PurposeTeacher] || !(Item{ClubOnly: true}).Offered(PurposeTeacher) || (Item{StaffOnly: true}).Offered(PurposeTeacher) {
		t.Fatal("teacher eligibility")
	}
}

// Teacher requests need their classroom details, checked with the other request rules.
func TestValidateTeacher(t *testing.T) {
	in := ValidateInput{
		Settings: Settings{RequestsOpen: true},
		Catalog:  map[int64]Item{1: {ID: 1, SKU: "Pri/A", Visible: true, ClubOnly: true, MaxPerRequest: 5}},
		Purpose:  PurposeTeacher, Reason: "Sharing with my class.", Lines: []Line{{ItemID: 1, Quantity: 1}}, Country: "US",
	}
	if err := Validate(in); err != ErrClassroom {
		t.Fatalf("no classroom = %v", err)
	}
	in.Classroom = &Classroom{School: "Example School", Grades: "9–12", Students: 30}
	if err := Validate(in); err != nil {
		t.Fatalf("teacher = %v", err)
	}
	in.Reason = " "
	if err := Validate(in); err != ErrNoReason {
		t.Fatalf("classroom must not replace the plan = %v", err)
	}
}
