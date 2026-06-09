Manage registered projects — list, get, resolve, and navigate

## List
  project list                 # all projects
  project list <org>           # filter by org
  project list --json          # JSON output

## Get
  project get <alias>          # show project details by alias

## Resolve
  project resolve <alias-or-path>  # resolve alias/path to alias, path, org, and GitHub token env

## Jump
  project jump <alias|org/repo>     # print filesystem path for a project
  project jump org/repo             # clones from GitHub if missing, then prints path

## Org
  project org list             # list all orgs
  project org get <name>       # show a single org
