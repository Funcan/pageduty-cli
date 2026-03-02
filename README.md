Pagerduty-CLI
=============

A golang CLI for pagerduty querying.

It allows you to query for incidents, services, users and more.

It is designed to always be read-only, so it is safe to run without worrying
about accidental modifications.

Uses the config file at `~/.pagerduty-cli/config.yaml` to store your API key.

Usage:

`pagerduty schedule next` - show me when I'm next on call
`pagerduty schedule next --user <user>` - show me when a specific user is next on call - search by name or email

`pagerduty scedule last` - show me when I was last on call
`pagerduty schedule last --user <user>` - show me when a specific user was last on call - search by name or email

`pagerduty schedule now` - show me who is currently on call for services I am on call for
`pagerduty schedule now --service <service>` - show me who is currently on call for a specific service - search by name

`pagerduty whoami` - show me details about my account - my configured name, email, and teams