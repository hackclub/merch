package swag

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Classroom is what a teacher tells us about who they'll share swag with. It is saved in the
// request note, visible to the requester and reviewers, and never sent to analytics or the warehouse.
type Classroom struct {
	School   string `json:"school"`
	Grades   string `json:"grades"`
	Students int    `json:"students"`
}

var ErrClassroom = errors.New("add your school, grades or ages, and approximate number of students")

// Check is the teacher request's rule (Validate calls it). Lengths count characters,
// like the form's maxlength.
func (c *Classroom) Check() error {
	if c == nil || !filled(c.School, 200) || !filled(c.Grades, 100) || c.Students < 1 || c.Students > 100000 {
		return ErrClassroom
	}
	return nil
}

func filled(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= max
}

// Note puts a labeled classroom summary before the teacher's plan, for the saved request note.
func (c *Classroom) Note(plan string) string {
	return fmt.Sprintf("School: %s\nGrades / ages: %s\nStudents: %d\n\n%s", strings.TrimSpace(c.School), strings.TrimSpace(c.Grades), c.Students, strings.TrimSpace(plan))
}
