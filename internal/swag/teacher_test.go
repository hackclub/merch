package swag

import "testing"

func TestClassroomNote(t *testing.T) {
	for _, c := range []*Classroom{nil, {}, {School: "School", Grades: "9", Students: 0}, {School: "School", Grades: " ", Students: 20}, {School: "School", Grades: "9", Students: 100001}} {
		if _, err := c.RequestNote("Plan"); err != ErrClassroom {
			t.Fatalf("invalid classroom accepted: %+v", c)
		}
	}
	c := &Classroom{School: " Example School ", Grades: " 9–12 ", Students: 30}
	if note, err := c.RequestNote(" Class introduction. "); err != nil || note != "School: Example School\nGrades / ages: 9–12\nStudents: 30\n\nClass introduction." {
		t.Fatalf("note = %q, %v", note, err)
	}
	if !Purposes[PurposeTeacher] || !(Item{ClubOnly: true}).Offered(PurposeTeacher) || (Item{StaffOnly: true}).Offered(PurposeTeacher) {
		t.Fatal("teacher eligibility")
	}
}
