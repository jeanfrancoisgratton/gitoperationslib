"""migrate-1.9.0.py PKG [build flags...]: moves a consumer's calls to the 1.9.0 API.

It rewrites calls to methods that left *Client, and Directory() calls, as the
compiler reports them ("X.M undefined (type ... Client ...)"), until it can fix
nothing more, then prints what is left for a person. Run it once per package,
from the module's directory:

    python3 -I migrate-1.9.0.py ./branch_mgmt/branches
    python3 -I migrate-1.9.0.py ./cmd -tags integration

It cannot see type assertions on interfaces that still exist (client.(gol.Runners)):
those compile and now always fail. It lists every type assertion on the library
at the end; replace each with the accessor (__docs/REFACTOR-1.9.0.md, Grid B) and,
where a capability check is wanted, client.Capabilities().Has(...).
"""
import os, re, subprocess, sys
M = {
 "Emails":"Account().Emails","SSHKeys":"Account().SSHKeys","GPGKeys":"Account().GPGKeys","Profile":"Account().Profile",
 "Tokens":"Account().Tokens","TokensOf":"Users().Tokens","UserWebhooks":"Account().Webhooks",
 "SystemWebhooks":"Site().SystemWebhooks","DefaultWebhooks":"Site().DefaultWebhooks",
 "Secrets":"CI().Secrets","Variables":"CI().Variables",
 "GetOrgSecretAccess":"CI().Secrets().GetAccess","SetOrgSecret":"CI().Secrets().SetWithAccess","SetOrgSecretRepos":"CI().Secrets().SetRepos",
 "CommitDiff":"Commits().Diff","CommitStatus":"Commits().Status","SetCommitStatus":"Commits().SetStatus",
 "PullRequestDiff":"PullRequests().Diff","PullRequestFiles":"PullRequests().Files","PullRequestCommits":"PullRequests().Commits",
 "ListReviewComments":"PullRequests().ListReviewComments","CreateReviewComment":"PullRequests().CreateReviewComment","DeleteReviewComment":"PullRequests().DeleteReviewComment",
 "ListHookDeliveries":"Webhooks().ListDeliveries","GetHookDelivery":"Webhooks().GetDelivery","RedeliverHook":"Webhooks().Redeliver",
 "LockIssue":"Issues().Lock","UnlockIssue":"Issues().Unlock",
 "ListOrgLabels":"Orgs().Labels().List","GetOrgLabel":"Orgs().Labels().Get","CreateOrgLabel":"Orgs().Labels().Create","EditOrgLabel":"Orgs().Labels().Edit","DeleteOrgLabel":"Orgs().Labels().Delete",
 "EditLabelByID":"Labels().EditByID","DeleteLabelByID":"Labels().DeleteByID",
 "ListGitHooks":"GitHooks().List","GetGitHook":"GitHooks().Get","SetGitHook":"GitHooks().Set","DeleteGitHook":"GitHooks().Delete",
 "ListCollaborators":"Collaborators().List","GetCollaborator":"Collaborators().Get","AddCollaborator":"Collaborators().Add","RemoveCollaborator":"Collaborators().Remove","ListCollaboratorInvitations":"Collaborators().ListInvitations",
 "ListPushMirrors":"Mirrors().ListPush","GetPushMirror":"Mirrors().GetPush","CreatePushMirror":"Mirrors().CreatePush","DeletePushMirror":"Mirrors().DeletePush","SyncPushMirrors":"Mirrors().SyncPush","SyncMirror":"Mirrors().Sync",
 "ListWorkflowRuns":"CI().Runs().List","GetWorkflowRun":"CI().Runs().Get","CancelWorkflowRun":"CI().Runs().Cancel","RerunWorkflowRun":"CI().Runs().Rerun","DeleteWorkflowRun":"CI().Runs().Delete",
 "ListWorkflowJobs":"CI().Runs().ListJobs","GetWorkflowJob":"CI().Runs().GetJob","RerunWorkflowJob":"CI().Runs().RerunJob","WorkflowJobLogs":"CI().Runs().JobLogs","WorkflowRunLogs":"CI().Runs().Logs",
 "FollowWorkflowJobLog":"CI().Runs().FollowJobLog",
 "ListWorkflowArtifacts":"CI().Artifacts().List","ListWorkflowRunArtifacts":"CI().Artifacts().ListForRun","GetWorkflowArtifact":"CI().Artifacts().Get","DeleteWorkflowArtifact":"CI().Artifacts().Delete","DownloadWorkflowArtifact":"CI().Artifacts().Download",
 "ListRunners":"CI().Runners().List","GetRunner":"CI().Runners().Get","RegisterRunner":"CI().Runners().Register","DeleteRunner":"CI().Runners().Delete","RunnerRegistrationToken":"CI().Runners().RegistrationToken",
 "SetOrgAvatar":"Orgs().SetAvatar","DeleteOrgAvatar":"Orgs().DeleteAvatar","ListUsersBy":"Users().Search",
}
D = {"Me":"Users().Me","User":"Users().Get","UserInfo":"Users().Info","Org":"Orgs().Get","ListOrgs":"Orgs().List","ListAllOrgs":"Orgs().ListAll",
     "OrgInfo":"Orgs().Info","OrgMembers":"Orgs().Members","ListTeams":"Teams().List","Team":"Teams().Get","TeamInfo":"Teams().Info",
     "TeamMembers":"Teams().Members","TeamRepos":"Teams().Repos"}
pkg, flags = sys.argv[1], sys.argv[2:]
for rnd in range(8):
    out = subprocess.run(["go","test","-c","-o","/dev/null","-gcflags=-e",*flags,pkg], capture_output=True, text=True).stderr
    errs = [l for l in out.splitlines() if re.match(r"\S+\.go:\d+:\d+:", l)]
    if not errs:
        print("clean"); break
    fixed = 0; left = []
    edits = {}
    for l in errs:
        m = re.match(r"(\S+\.go):(\d+):(\d+): (.*)", l)
        f, ln, col, msg = m.group(1), int(m.group(2)), int(m.group(3)), m.group(4)
        mm = re.match(r"(.+)\.(\w+) undefined \(type .*Client", msg)
        md = re.match(r"(.+)\.Directory undefined \(type .*Client", msg)
        if md:
            edits.setdefault(f, []).append((ln, "dir", None)); continue
        if mm and mm.group(2) in M:
            edits.setdefault(f, []).append((ln, mm.group(2), (M[mm.group(2)], col))); continue
        left.append(l)
    for f, es in edits.items():
        L = open(f).read().split("\n")
        for ln, old, new in es:
            line = L[ln-1]
            ma = re.match(r"^(\s*)(\w+) :?= (.+)\.Directory\(\)\s*$", line) if old == "dir" else None
            if ma:
                # v := client.Directory(): calls on v go to the client
                v, recv = ma.group(2), ma.group(3)
                L[ln-1] = "\x00"
                rest = "\n".join(L)
                rest = re.sub(r"\b%s\.(\w+)\(" % re.escape(v), lambda m: recv+"."+D.get(m.group(1), "Directory()."+m.group(1))+"(", rest)
                L = rest.split("\n"); fixed += 1
                continue
            if old == "dir":
                line2 = re.sub(r"\.Directory\(\)\.(\w+)\(", lambda m: "."+D.get(m.group(1), "Directory()."+m.group(1))+"(", line)
            else:
                target, c0 = new
                k = line.find("."+old+"(", max(0, c0-2))
                line2 = line if k < 0 else line[:k] + "." + target + "(" + line[k+len(old)+2:]
            if line2 != line:
                L[ln-1] = line2; fixed += 1
        open(f, "w").write("\n".join(l for l in L if l != "\x00"))
    print("round", rnd, "fixed", fixed, "left", len(left))
    if fixed == 0:
        print("\n".join(left[:60])); break

# Type assertions on the library compile but no longer find anything.
pkgdir = pkg.rstrip("/.")
for root, _, files in os.walk(pkgdir or "."):
    for name in files:
        if name.endswith(".go"):
            path = os.path.join(root, name)
            for i, line in enumerate(open(path).read().split("\n"), 1):
                if re.search(r"\.\((gol|gitoperationslib)\.\w+\)", line):
                    print("type assertion: %s:%d: %s" % (path, i, line.strip()))
