Review the changes in the branch `git diff $(git symbolic-ref refs/remotes/origin/HEAD | sed 's@^refs/remotes/@@')...HEAD` carefully:

* Find logic flaws
* Find low-quality code: useless branches, defensive programming, duplicated code
* Find comments that are not helpful: trivia, obvious, stale

After the code review, make the next actions if necessary.
