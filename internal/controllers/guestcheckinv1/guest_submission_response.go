package guestcheckinv1

import (
	"time"

	"kids-checkin/internal/repo/guestsubmission"
)

type Parent struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2"`
	City      string `json:"city"`
	State     string `json:"state"`
	Zip       string `json:"zip"`
}

type Child struct {
	FirstName           string `json:"first_name"`
	LastName            string `json:"last_name"`
	DOB                 string `json:"dob"`
	Grade               string `json:"grade"`
	Gender              string `json:"gender"`
	DietaryRestrictions string `json:"dietary_restrictions"`
	SpecialNeeds        string `json:"special_needs"`
	Relationship        string `json:"relationship"`
}

type Submission struct {
	PublicID  string     `json:"public_id"`
	Status    string     `json:"status"`
	Parent    Parent     `json:"parent"`
	Children  []Child    `json:"children"`
	SafetyAck bool       `json:"safety_ack"`
	CreatedAt *time.Time `json:"created_at"`
}

type ParentSummary struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type ChildSummary struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type SubmissionSummary struct {
	PublicID  string         `json:"public_id"`
	Status    string         `json:"status"`
	Parent    ParentSummary  `json:"parent"`
	Children  []ChildSummary `json:"children"`
	CreatedAt *time.Time     `json:"created_at"`
}

type SubmissionPage struct {
	Items      []Submission `json:"items"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	PageSize   int          `json:"page_size"`
	TotalPages int          `json:"total_pages"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func submissionToResponse(s guestsubmission.Submission) Submission {
	children := make([]Child, 0, len(s.Children))
	for _, child := range s.Children {
		children = append(children, Child{
			FirstName:           child.FirstName,
			LastName:            child.LastName,
			DOB:                 child.DOB,
			Grade:               child.Grade,
			Gender:              child.Gender,
			DietaryRestrictions: child.DietaryRestrictions,
			SpecialNeeds:        child.SpecialNeeds,
			Relationship:        child.Relationship,
		})
	}
	return Submission{
		PublicID:  s.PublicID,
		Status:    s.Status,
		Parent:    Parent{FirstName: s.Parent.FirstName, LastName: s.Parent.LastName, Phone: s.Parent.Phone, Email: s.Parent.Email, Address1: s.Parent.Address1, Address2: s.Parent.Address2, City: s.Parent.City, State: s.Parent.State, Zip: s.Parent.Zip},
		Children:  children,
		SafetyAck: s.SafetyAck,
		CreatedAt: timePtr(s.CreatedAt),
	}
}

func submissionsToResponse(subs []guestsubmission.Submission) []Submission {
	out := make([]Submission, 0, len(subs))
	for _, s := range subs {
		out = append(out, submissionToResponse(s))
	}
	return out
}

func submissionToSummary(s guestsubmission.Submission) SubmissionSummary {
	children := make([]ChildSummary, 0, len(s.Children))
	for _, child := range s.Children {
		children = append(children, ChildSummary{FirstName: child.FirstName, LastName: child.LastName})
	}
	return SubmissionSummary{
		PublicID:  s.PublicID,
		Status:    s.Status,
		Parent:    ParentSummary{FirstName: s.Parent.FirstName, LastName: s.Parent.LastName},
		Children:  children,
		CreatedAt: timePtr(s.CreatedAt),
	}
}

func submissionsToSummary(subs []guestsubmission.Submission) []SubmissionSummary {
	out := make([]SubmissionSummary, 0, len(subs))
	for _, s := range subs {
		out = append(out, submissionToSummary(s))
	}
	return out
}
