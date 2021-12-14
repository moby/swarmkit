# Contributing to GoCSI
The GoCSI project welcomes and depends on contributions from the open
source community. Whether a developer or user, contributions are not
just source code:

- Code patches via pull requests
- Documentation improvements
- Bug reports and patch reviews

## Reporting an Issue
Please include as much detail as you can. This includes:

  * The OS type and version
  * The CSI API version
  * The GoCSI version
  * A set of logs with debug-logging enabled that show the problem

## Testing the Development Version
The quickest way to use the bleeding edge copy of GoCSI or one of its
components is with the following command:

```shell
$ go get -u github.com/rexray/gocsi
```

## Running the tests
The following command executes GoCSI's test suite:

```shell
$ go test ./testing
```

## Submitting Changes
Changes to GoCSI are accepted via GitHub pull requests. All changes are
reviewed prior to acceptance, and if source code has been changed please
remember to update the related tests.

All developers are required to follow the
[GitHub Flow model](https://guides.github.com/introduction/flow/) when
proposing new features or even submitting fixes.

Please note that although not explicitly stated in the referenced GitHub Flow
model, all work should occur on a __fork__ of this project, not from within a
branch of this project itself.

Pull requests submitted to this project should adhere to the following
guidelines:

  * Branches should be rebased off of the upstream master prior to being
    opened as pull requests and again prior to merge. This is to ensure that
    the build system accounts for any changes that may only be detected during
    the build and test phase.

  * Unless granted an exception a pull request should contain only a single
    commit. This is because features and patches should be atomic -- wholly
    shippable items that are either included in a release, or not. Please
    squash commits on a branch before opening a pull request. It is not a
    deal-breaker otherwise, but please be prepared to add a comment or
    explanation as to why you feel multiple commits are required.

People contributing code to this project must adhere to the following rules.
These standards are in place to keep code clean, consistent, and stable.

## Documentation
There are two types of documentation: source and markdown.

### Source Code
All source code should be documented in accordance with the
[Go's documentation rules](http://blog.golang.org/godoc-documenting-go-code).

### Markdown
When creating or modifying the project's `README.md` file or any other
Markdown documentation, please keep the following rules in mind:

1. All markdown should be limited to a width of 80 characters. This makes
the document easier to read in text editors. GitHub still produces the
proper result when parsing the markdown.
2. All links to internal resources should be relative.
3. All links to markdown files should include the file extension.

## Style & Syntax
All source files should be processed by the following tools prior to being
committed. Any errors or warnings produced by the tools should be corrected
before the source is committed.

Tool | Description
-----|------------
[gofmt](https://golang.org/cmd/gofmt/) | A golang source formatting tool
[golint](https://github.com/golang/lint) | A golang linter
[govet](https://golang.org/cmd/vet/) | A golang source optimization tool
[gocyclo](https://github.com/fzipp/gocyclo) | A golang cyclomatic complexity detection tool. No function should have a score above 0.15

The [go-plus](https://atom.io/packages/go-plus) plug-in is available for
the [Atom](https://atom.io/) IDE and will handle all of the above tooling
automatically.

Another option is to use a client-side, pre-commit hook to ensure that the
sources meet the required standards. For example, in the project's `.git/hooks`
directory create a file called `pre-commit` and mark it as executable. Then
paste the following content inside the file:

```shell
#!/bin/sh
gofmt -w $(find . -name "*.go" | grep -v ./vendor)
```

The above script will execute prior to a Git commit operation, prior to even
the commit message dialog. The script will invoke `gofmt` for all of the
project's Go source files. If the command returns a non-zero exit code,
the commit operation will abort with the error.

## Commit Messages
Commit messages should follow the guide [5 Useful Tips For a Better Commit
Message](https://robots.thoughtbot.com/5-useful-tips-for-a-better-commit-message).
The two primary rules to which to adhere are:

  1. Commit message subjects should not exceed 50 characters in total and
     should be followed by a blank line.

  2. The commit message's body should not have a width that exceeds 72
     characters.

For example, the following commit has a very useful message that is succinct
without losing utility.

```text
commit b6498b3ecf290b77666f80e3c237ea59b6d569d7
Author: akutz <sakutz@gmail.com>
Date:   Mon Feb 5 19:33:03 2018 -0600

    Spec Validation for Response Msgs - Off & Detailed

    This patch updates the way GoCSI's specification validation middleware
    handles response validation:

    * Validating response messages against the CSI specification now is now
      disabled by default. Use `X_CSI_SPEC_REP_VALIDATION=true` to enable
      response validation.
    * When enabled, response validation no longer drops the response
      message. Instead the message is encoded into a gRPC error's `Details`
      field and the error code is set to `Internal`.

    The test `With Invalid Plug-in Name Error` in the file
    `./testing/identity_test.go` demonstrates how to reconstitute the
    response data on the client-side when the server-side middleware marks
    the response as invalid.

    This is a necessary and important change. An SP is capable of making
    changes to an underlying storage platform. The results of such changes
    are reflected in CSI response messages. Dropping these messages could
    render a client unable to acknowledge a change has occurred, and could
    potentially lead to further errors, such as data loss. This change
    both allows for response message validation and still enables clients
    to reconstitute the response data, even when it is marked in error.
```

Please note that the output above is the full output for viewing a commit.
However, because the above message adheres to the commit message rules, it's
quite easy to show just the commit's subject:

```shell
$ git show b6498b3ecf290b77666f80e3c237ea59b6d569d7 --format="%s" -s
Spec Validation for Response Msgs - Off & Detailed
```

It's also equally simple to print the commit's subject and body together:

```shell
$ git show b6498b3ecf290b77666f80e3c237ea59b6d569d7 --format="%s%n%n%b" -s
Spec Validation for Response Msgs - Off & Detailed

This patch updates the way GoCSI's specification validation middleware
handles response validation:

* Validating response messages against the CSI specification now is now
  disabled by default. Use `X_CSI_SPEC_REP_VALIDATION=true` to enable
  response validation.
* When enabled, response validation no longer drops the response
  message. Instead the message is encoded into a gRPC error's `Details`
  field and the error code is set to `Internal`.

The test `With Invalid Plug-in Name Error` in the file
`./testing/identity_test.go` demonstrates how to reconstitute the
response data on the client-side when the server-side middleware marks
the response as invalid.

This is a necessary and important change. An SP is capable of making
changes to an underlying storage platform. The results of such changes
are reflected in CSI response messages. Dropping these messages could
render a client unable to acknowledge a change has occurred, and could
potentially lead to further errors, such as data loss. This change
both allows for response message validation and still enables clients
to reconstitute the response data, even when it is marked in error.
```

## Sign Your work
The sign-off is a simple line at the end of the explanation for the patch. Your
signature certifies that you wrote the patch or otherwise have the right to pass
it on as an open-source patch. The rules are pretty simple: if you can certify
the below (from [developercertificate.org](http://developercertificate.org/)):

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
1 Letterman Drive
Suite D4700
San Francisco, CA, 94129

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Then you just add a line to every git commit message:

    Signed-off-by: Joe Smith <joe.smith@email.com>

Use your real name (sorry, no pseudonyms or anonymous contributions.)

If you set your `user.name` and `user.email` git configs, you can sign your
commit automatically with `git commit -s`.
