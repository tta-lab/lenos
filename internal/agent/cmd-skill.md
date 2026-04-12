Discover and read agent skills from the filesystem

## List
  skill list                        # show all discovered skills (name, category, source, description)

## Get
Read a skill's full instructions:
  skill get <name>                  # print skill body to stdout (frontmatter stripped)

## Find
  skill find <keyword>...           # search by keyword (OR match across name and description)

## Discovery
Skills are directories containing a SKILL.md file with YAML frontmatter.
Discovery walks these paths in priority order (first match wins):
  1. {cwd}/.agents/skills
  2. {cwd}/.crush/skills
  3. {cwd}/.claude/skills
  4. {cwd}/.cursor/skills
  5. ~/.agents/skills
  6. ~/.crush/skills
  7. ~/.claude/skills
  8. ~/.cursor/skills
