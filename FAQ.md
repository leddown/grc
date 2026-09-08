# FAQ

## How do I merge `security-patches` back into `main` after testing?

Once `security-patches` is fully tested and approved, run:

```bash
git checkout main
git pull origin main
git merge --no-ff security-patches
go test ./...
git push origin main
```

Recommended follow-up:

```bash
git branch -d security-patches
git push origin --delete security-patches
```

If the branch was merged through a GitHub Pull Request, use the PR merge button, then run:

```bash
git checkout main
git pull origin main
```
