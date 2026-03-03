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

`pageduty report` [--from <date>] [--to <date>]
                  [--team <team>]
                  [--service <service>] 
    - generate a markdown formatted report of incidents - list of all incidents that got assigned to my team(s) in the time frame, who acked them, and any notes added. Defaults to from the beginning to the end of my last shift
    **(This command can take several tens of seconds to complete do to extensive use of pagerduty APIs)**

`pagerduty status [--team <team>] [--service <service>]` - print the currently unresolved incidents, with their acknowledgement status and any notes
