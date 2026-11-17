package swag

import (
	"errors"
	"fmt"
	"strings"
)

// Classroom details stay in the request note, visible to the requester and reviewers.
// They are never sent to analytics or the warehouse.
type Classroom struct {
	School   string `json:"school"`
	Grades   string `json:"grades"`
	Students int    `json:"students"`
}

var ErrClassroom = errors.New("add your school, grades or ages, and approximate number of students")

func (c *Classroom) RequestNote(note string) (string, error) {
	if c == nil || strings.TrimSpace(c.School) == "" || len(c.School) > 200 || strings.TrimSpace(c.Grades) == "" || len(c.Grades) > 100 || c.Students < 1 || c.Students > 100000 {
		return "", ErrClassroom
	}
	return fmt.Sprintf("School: %s\nGrades / ages: %s\nStudents: %d\n\n%s", strings.TrimSpace(c.School), strings.TrimSpace(c.Grades), c.Students, strings.TrimSpace(note)), nil
}
