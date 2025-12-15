Review the changes in the branch `git diff $(git symbolic-ref refs/remotes/origin/HEAD | sed 's@^refs/remotes/@@')...HEAD` carefully.

This branch is intended to fix a issue, so understand the issue first, and then recognize the resolution:

* There should be a minimum reproducible test case for the issue.
* Logic flaws are not allowed.
* Comments that are trivial, obvious or stale are not allowed.
