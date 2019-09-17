# gomr

Go mod replace (gomr) manager. Easily add and remove replace lines to a module.
It stores the replace lines you add so you can remove them all at once or
re-add them with a single command.

Example:

```bash
# Normally you must provide a second argument for the replace path
# But in this case it's in my GOPATH at $GOPATH/src/github.com/aarondl/gitio
# so it will use that checked out copy.
# This does three things:
# - Add replace line
# - Stores the replace line for later
# - Adds an empty go.mod to the directory since it doesn't exist and is required
gomr add github.com/aarondl/gitio

# Removes all the replace lines that were recorded in the .gomr file
# It also removes any empty go.mod's that were installed as part of creating
# the replace.
gomr down

# Adds all the replace lines back to go.mod as well as installs all the empty
# go.mods so that everything works again.
gomr up

# Removes the recorded .gomr replace line so it's no longer affected by up/down
# Removes the empty go.mod if one had been added
# Removes the replace line from go.mod so it uses the module cache again
gomr remove github.com/aarondl/gitio
```
