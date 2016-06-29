Jira is an acme based client for jira.

Write support is currently dodgy.  It's aiming for rough feature parity
with `github.com/rsc/github/issue`.

Some code cribbed from `github.com/rsc/github/issue`.

Jira can be started from the plumber. An example rule:

	type is text
	data matches '[A-Z]+-[0-9]+'
	data matches '((CORP)|(ABC))-[0-9]+'
	plumb to jira
	plumb client Jira https://corp.atlassian.net

The 'p' flag will disable attempting to talk to the plumber, and will make
Jira exit after all of its windows are closed. Sending a plumber message
of type `exit` will cause Jira to close all of its windows and exit.
