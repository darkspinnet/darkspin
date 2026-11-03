package main

type ServerIdentityConfiguration struct {
	Revision    string `json:"revision"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Welcome     string `json:"welcome"`
	Rules       string `json:"rules"`
	ContactURL  string `json:"contact_url"`
}
