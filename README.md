# [SwarmKit](https://github.com/docker/swarmkit)

[![GoDoc](https://godoc.org/github.com/docker/swarmkit?status.png)](https://godoc.org/github.com/docker/swarmkit)
[![Circle CI](https://circleci.com/gh/docker/swarmkit.svg?style=shield&circle-token=a7bf494e28963703a59de71cf19b73ad546058a7)](https://circleci.com/gh/docker/swarmkit)
[![codecov.io](https://codecov.io/github/docker/swarmkit/coverage.svg?branch=master&token=LqD1dzTjsN)](https://codecov.io/github/docker/swarmkit?branch=master)
[![Badge Badge](http://doyouevenbadge.com/github.com/docker/swarmkit)](http://doyouevenbadge.com/report/github.com/docker/swarmkit)

*SwarmKit* is a toolkit for orchestrating distributed systems at any scale. It includes primitives for node discovery, raft-based consensus, task scheduling and more.

Its main benefits are:

- **Distributed**: *SwarmKit* uses the [Raft Consensus Algorithm](https://raft.github.io/) in order to coordinate and does not rely on a single point of failure to perform decisions.
- **Secure**: Node communication and membership within a *Swarm* are secure out of the box. *SwarmKit* uses mutual TLS for node *authentication*, *role authorization* and *transport encryption*, automating both certificate issuance and rotation.
- **Simple**: *SwarmKit* is operationally simple and minimizes infrastructure dependencies. It does not need an external database to operate.

## Overview

Machines running *SwarmKit* can be grouped together in order to form a *Swarm*, coordinating tasks with each other.
Once a machine joins, it becomes a *Swarm Node*. Nodes can either be *worker* nodes or *manager* nodes.

- **Worker Nodes** are responsible for running Tasks using an *Executor*. *SwarmKit* comes with a default *Docker Container Executor* that can be easily swapped out.
- **Manager Nodes** on the other hand accept specifications from the user and are responsible for reconciling the desired state with the actual cluster state.

An operator can dynamically update a Node's role by promoting a Worker to Manager or demoting a Manager to Worker.

*Tasks* are organized in *Services*. A service is a higher level abstraction that allows the user to declare the desired state of a group of tasks.
Services define what type of task should be created as well as how to execute them (e.g. run this many replicas at all times) and how to update them (e.g. rolling updates).

## Features

Some of *SwarmKit*'s main features are:

- **Orchestration**
  - **Desired State Reconciliation**: *SwarmKit* constantly compares the desired state against the current cluster state
    and reconciles the two if necessary. For instance, if a node fails, *SwarmKit* reschedules its tasks onto a different node.
  - **Service Types**: There are different types of services. The project currently ships with two of them out of the box:
    - **Replicated Services** are scaled to the desired number of replicas.
    - **Global Services** run one task on every available node in the cluster.
  - **Configurable Updates**: At any time, you can change the value of one or more fields for a service.
    After you make the update, *SwarmKit* reconciles the desired state by ensuring all tasks are using the desired settings.
    By default, it performs a lockstep update - that is, update all tasks at the same time. This can be configured through
    different knobs:
    - **Parallelism** defines how many updates can be performed at the same time.
    - **Delay** sets the minimum delay between updates. *SwarmKit* will start by shutting down the previous task, bring up a new one,
      wait for it to transition to the *RUNNING* state *then* wait for the additional configured delay.
      Finally, it will move onto other tasks.
  - **Restart Policies**: The orchestration layer monitors tasks and reacts to failures based on the specified policy.
    The operator can define restart conditions, delays and limits (maximum number of attempts in a given time window).
    *SwarmKit* can decide to restart a task on a different machine. This means that faulty nodes will gradually be drained of their
    tasks.
- **Scheduling**
  - **Resource Awareness**: *SwarmKit* is aware of resources available on nodes and will place tasks accordingly.
  - **Constraints**: Operators can limit the set of nodes where a task can be scheduled by defining constraint expressions.
    Multiple constraints find nodes that satisfy every expression, i.e., an `AND` match. Constraints can match node attributes in the following table.
		Note that `engine.labels` are collected from Docker Engine with information like operating system,
 drivers, etc. `node.labels` are added by cluster administrators for operational purpose.
For example, some nodes have security compliant labels to run tasks with compliant requirements.

| node attribute | matches | example |
|:------------- |:-------------| :-------------|
| node.id | node's ID | `node.id == 2ivku8v2gvtg4`|
| node.hostname | node's hostname | `node.hostname != node-2`|
| node.role |  node's manager or worker role | `node.role == manager`|
| node.labels | node's labels added by cluster admins | `node.labels.security == high`|
| engine.labels | Docker Engine's labels | `engine.labels.operatingsystem == ubuntu 14.04`|

  - **Strategies**: The project currently ships with a *spread strategy* which will attempt to schedule tasks on the least loaded
    nodes, provided they meet the constraints and resource requirements.
- **Cluster Management**
  - **State Store**: Manager nodes maintain a strongly consistent, replicated (Raft based) and extremely fast (in-memory reads)
    view of the cluster which allows them to make quick scheduling decisions while tolerating failures.
  - **Topology Management**: Node roles (*Worker* / *Manager*) can be dynamically changed through API/CLI calls.
  - **Node Management**: An operator can alter the desired availability of a node: Setting it to *Paused* will prevent any further
    tasks from being scheduled to it while *Drained* will have the same effect while also re-scheduling its tasks somewhere else
    (mostly for maintenance scenarios).
- **Security**
  - **Mutual TLS**: All nodes communicate with each other using mutual *TLS*. Swarm managers act as a *Root Certificate Authority*,
    issuing certificates to new nodes.
  - **Acceptance Policy**: Policies can be put in place to auto accept, manually accept, or require a secret to join the cluster.
  - **Certificate Rotation**: TLS Certificates are rotated and reloaded transparently on every node, allowing a user to set how
    frequently rotation should happen (the current default is 3 months, the minimum is 30 minutes).

## Build

Requirements:

- Go 1.6 or higher
- A [working golang](https://golang.org/doc/code.html) environment

*SwarmKit* is built in Go and leverages a standard project structure to work well with Go tooling.
If you are new to Go, please see [BUILDING.md](BUILDING.md) for a more detailed guide.

Once you have *SwarmKit* checked out in your `$GOPATH`, the `Makefile` can be used for common tasks.

From the project root directory, run the following to build `swarmd` and `swarmctl`:

```
make binaries
```

## Test

Before running tests for the first time, setup the tooling:

```bash
$ make setup
```

Then run:

```bash
$ make all
```

## Usage Examples

### Setting up a Swarm

These instructions assume that `swarmd` and `swarmctl` are in your PATH.

(Before starting, make sure `/tmp/node-N` don't exist)

Initialize the first node:

```sh
$ swarmd -d /tmp/node-1 --listen-control-api /tmp/manager1/swarm.sock --hostname node-1
```

In two additional terminals, join two nodes (note: replace `127.0.0.1:4242` with the address of the first node)

```sh
$ swarmd -d /tmp/node-2 --hostname node-2 --join-addr 127.0.0.1:4242
$ swarmd -d /tmp/node-3 --hostname node-3 --join-addr 127.0.0.1:4242
```

In a fourth terminal, use `swarmctl` to explore and control the cluster. Before
running swarmctl, set the `SWARM_SOCKET` environment variable to the path of the
manager socket that was specified in `--listen-control-api` when starting the
manager.

To list nodes:

```
$ export SWARM_SOCKET=/tmp/manager1/swarm.sock
$ swarmctl node ls
ID             Name    Membership  Status  Availability  Manager status
--             ----    ----------  ------  ------------  --------------
15jkw04qb4yze  node-1  ACCEPTED    READY   ACTIVE        REACHABLE *
1zbwraf2v8hpx  node-3  ACCEPTED    READY   ACTIVE
3vj01av6782qn  node-2  ACCEPTED    READY   ACTIVE


```

### Creating Services

Start a *redis* service:

```
$ swarmctl service create --name redis --image redis:3.0.5
89831rq7oplzp6oqcqoswquf2
```

List the running services:

```
$ swarmctl service ls
ID                         Name   Image        Replicas
--                         ----   -----        ---------
89831rq7oplzp6oqcqoswquf2  redis  redis:3.0.5  1
```

Inspect the service:

```
$ swarmctl service inspect redis
ID                : 89831rq7oplzp6oqcqoswquf2
Name              : redis
Replicass         : 1
Template
 Container
  Image           : redis:3.0.5

Task ID                      Service    Instance    Image          Desired State    Last State               Node
-------                      -------    --------    -----          -------------    ----------               ----
0dsiq9za9at3cqk4qx07n6v8j    redis      1           redis:3.0.5    RUNNING          RUNNING 2 seconds ago    node-1
```

### Updating Services

You can update any attribute of a service.

For example, you can scale the service by changing the instance count:

```
$ swarmctl service update redis --replicas 6
89831rq7oplzp6oqcqoswquf2

$ swarmctl service inspect redis
ID                : 89831rq7oplzp6oqcqoswquf2
Name              : redis
Replicas          : 6
Template
 Container
  Image           : redis:3.0.5

Task ID                      Service    Instance    Image          Desired State    Last State               Node
-------                      -------    --------    -----          -------------    ----------               ----
0dsiq9za9at3cqk4qx07n6v8j    redis      1           redis:3.0.5    RUNNING          RUNNING 1 minute ago     node-1
9fvobwddp5ve3k0f4al1mhuhn    redis      2           redis:3.0.5    RUNNING          RUNNING 3 seconds ago    node-2
e7pxax9mhjd4zamohobefqpy0    redis      3           redis:3.0.5    RUNNING          RUNNING 3 seconds ago    node-2
ceuwhcffcavur7k9q57vqw0zg    redis      4           redis:3.0.5    RUNNING          RUNNING 3 seconds ago    node-1
8vqmbo95l6obbtb7fpmvz522f    redis      5           redis:3.0.5    RUNNING          RUNNING 3 seconds ago    node-3
385utv15nalm2pyupao6jtu12    redis      6           redis:3.0.5    RUNNING          RUNNING 3 seconds ago    node-3
```

Changing *replicas* from *1* to *6* forced *SwarmKit* to create *5* additional Tasks in order to
comply with the desired state.

Every other field can be changed as well, such as image, args, env, ...

Let's change the image from *redis:3.0.5* to *redis:3.0.6* (e.g. upgrade):

```
$ swarmctl service update redis --image redis:3.0.6
89831rq7oplzp6oqcqoswquf2

$ swarmctl service inspect redis
ID                : 89831rq7oplzp6oqcqoswquf2
Name              : redis
Replicas          : 6
Template
 Container
  Image           : redis:3.0.6

Task ID                      Service    Instance    Image          Desired State    Last State                Node
-------                      -------    --------    -----          -------------    ----------                ----
7947mlunwz2dmlet3c7h84ln3    redis      1           redis:3.0.6    RUNNING          RUNNING 34 seconds ago    node-3
56rcujrassh7tlljp3k76etyw    redis      2           redis:3.0.6    RUNNING          RUNNING 34 seconds ago    node-1
8l7bwrduq80pkq9tu4bsd95p4    redis      3           redis:3.0.6    RUNNING          RUNNING 36 seconds ago    node-2
3xb1jxytdo07mqccadt06rgi0    redis      4           redis:3.0.6    RUNNING          RUNNING 34 seconds ago    node-1
16aate5akcimsye9cp5xis1ih    redis      5           redis:3.0.6    RUNNING          RUNNING 34 seconds ago    node-2
dws408a3gz0zx0bygq3aj0ztk    redis      6           redis:3.0.6    RUNNING          RUNNING 34 seconds ago    node-3
```

By default, all tasks are updated at the same time.

This behavior can be changed by defining update options.

For instance, in order to update tasks 2 at a time and wait at least 10 seconds between updates:

```
$ swarmctl service update redis --image redis:3.0.7 --update-parallelism 2 --update-delay 10s
$ watch -n1 "swarmctl service inspect redis"  # watch the update
```

This will update 2 tasks, wait for them to become *RUNNING*, then wait an additional 10 seconds before moving to other tasks.

Update options can be set at service creation and updated later on. If an update command doesn't specify update options, the last set of options will be used.

### Node Management

*SwarmKit* monitors node health. In the case of node failures, it re-schedules tasks to other nodes.

An operator can manually define the *Availability* of a node and can *Pause* and *Drain* nodes.

Let's put `node-1` into maintenance mode:

```
$ swarmctl node drain node-1

$ swarmctl node ls
ID             Name    Membership  Status  Availability  Manager status
--             ----    ----------  ------  ------------  --------------
2o8evbttw2sjj  node-1  ACCEPTED    READY   DRAIN         REACHABLE
2p7w0q83jargg  node-2  ACCEPTED    READY   ACTIVE        REACHABLE *
3ieflj99g4wh8  node-3  ACCEPTED    READY   ACTIVE        REACHABLE

$ swarmctl service inspect redis
ID                : 89831rq7oplzp6oqcqoswquf2
Name              : redis
Replicas          : 6
Template
 Container
  Image           : redis:3.0.7

Task ID                      Service    Instance    Image          Desired State    Last State               Node
-------                      -------    --------    -----          -------------    ----------               ----
2pbjiykmaltiujokm0r8hmpz4    redis      1           redis:3.0.7    RUNNING          RUNNING 1 minute ago     node-2
az8ias15auf6w11jndsk7bc2o    redis      2           redis:3.0.7    RUNNING          RUNNING 1 minute ago     node-3
5gsogy426bnqxdfynheqcqdls    redis      3           redis:3.0.7    RUNNING          RUNNING 4 seconds ago    node-2
6vfzoshzb4jhyvp59yuf4dtnj    redis      4           redis:3.0.7    RUNNING          RUNNING 5 seconds ago    node-3
18p0ei3a43xermxsnvvv0v1vd    redis      5           redis:3.0.7    RUNNING          RUNNING 2 minutes ago    node-2
70eln8ibd8aku6jvmu8xz3hbc    redis      6           redis:3.0.7    RUNNING          RUNNING 4 seconds ago    node-3
```

As you can see, every Task running on `node-1` was rebalanced to either `node-2` or `node-3` by the reconciliation loop.
