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

```bash
$ make binaries
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

```bash
$ swarmd -d /tmp/node-1 --listen-control-api /tmp/node-1/swarm.sock --hostname node-1
```

Before joining cluster, the token should be fetched first:  

```  
$ export SWARM_SOCKET=/tmp/node-1/swarm.sock  
$ swarmctl cluster inspect default  
ID          : 9cwn0cir4hnal8suzseiiub6m
Name        : default
Orchestration settings:
  Task history entries: 5
Dispatcher settings:
  Dispatcher heartbeat period: 5s
Certificate Authority settings:
  Certificate Validity Duration: 2160h0m0s
  Join Tokens:
    Worker: SWMTKN-1-65nd131bc3m2bchdg915lgozedrpygyx2y1u6phy3s84bgbxva-bg0adlhzstyskk4gvowz467n9
    Manager: SWMTKN-1-65nd131bc3m2bchdg915lgozedrpygyx2y1u6phy3s84bgbxva-ds9xgn26j8tjhp1ftb13zjptm
```

In two additional terminals, join two nodes (note: replace `127.0.0.1:4242` with the address of the first node, and use the `<Worker TOKEN>` acquired above. In this example, the `<Worker TOKEN>` is `SWMTKN-1-65nd131bc3m2bchdg915lgozedrpygyx2y1u6phy3s84bgbxva-bg0adlhzstyskk4gvowz467n`):  

```sh
$ swarmd -d /tmp/node-2 --hostname node-2 --join-addr 127.0.0.1:4242 --join-token <Worker TOKEN>
$ swarmd -d /tmp/node-3 --hostname node-3 --join-addr 127.0.0.1:4242 --join-token <Worker TOKEN>
```

In a fourth terminal, use `swarmctl` to explore and control the cluster. Before
running swarmctl, set the `SWARM_SOCKET` environment variable to the path of the
manager socket that was specified in `--listen-control-api` when starting the
manager.

To list nodes:

```
$ export SWARM_SOCKET=/tmp/node-1/swarm.sock
$ swarmctl node ls
ID                         Name    Membership  Status  Availability  Manager Status
--                         ----    ----------  ------  ------------  --------------
7cna6u5nqcmi7moe9qnjzrss5  node-1  ACCEPTED    READY   ACTIVE        REACHABLE *
cr962qysr3xr6k3ed5a5qjsde  node-3  ACCEPTED    READY   ACTIVE
du359trx1fnyhztwwpjlpa9yl  node-2  ACCEPTED    READY   ACTIVE
```

### Creating Services

Start a *redis* service:

```
$ swarmctl service create --name redis --image redis:3.0.5
dy0vwwae8ropb5skvzcekzk6o
```

List the running services:

```
$ swarmctl service ls
ID                         Name   Image        Replicas
--                         ----   -----        --------
dy0vwwae8ropb5skvzcekzk6o  redis  redis:3.0.5  0/1
```

Inspect the service:

```
$ swarmctl service inspect redis
ID                : dy0vwwae8ropb5skvzcekzk6o
Name              : redis
Replicas          : 0/1
Template
 Container
  Image           : redis:3.0.5

Task ID                      Service    Slot    Image          Desired State    Last State                Node
-------                      -------    ----    -----          -------------    ----------                ----
cbbsslmcj9wgk9uprgmyf9312    redis      1       redis:3.0.5    RUNNING          PREPARING 1 minute ago    node-1
```

### Updating Services

You can update any attribute of a service.

For example, you can scale the service by changing the instance count:

```
$ swarmctl service update redis --replicas 6
dy0vwwae8ropb5skvzcekzk6o

$ swarmctl service inspect redis
ID                : dy0vwwae8ropb5skvzcekzk6o
Name              : redis
Replicas          : 6/6
Template
 Container
  Image           : redis:3.0.5

Task ID                      Service    Slot    Image          Desired State    Last State                Node
-------                      -------    ----    -----          -------------    ----------                ----
cbbsslmcj9wgk9uprgmyf9312    redis      1       redis:3.0.5    RUNNING          RUNNING 55 seconds ago    node-1
77csqgps2ffunkmfpwhn37j21    redis      2       redis:3.0.5    RUNNING          RUNNING 21 seconds ago    node-2
4rx5dbomdl86kbh0j406dyu5d    redis      3       redis:3.0.5    RUNNING          RUNNING 21 seconds ago    node-1
92pp9y9bcctddn9wfm5bbbyds    redis      4       redis:3.0.5    RUNNING          RUNNING 21 seconds ago    node-3
1olk8gw0o11twszzvafygtt8l    redis      5       redis:3.0.5    RUNNING          RUNNING 21 seconds ago    node-3
0llx4vk2ns94tjesxo5743qg8    redis      6       redis:3.0.5    RUNNING          RUNNING 21 seconds ago    node-2
```

Changing *replicas* from *1* to *6* forced *SwarmKit* to create *5* additional Tasks in order to
comply with the desired state.

Every other field can be changed as well, such as image, args, env, ...

Let's change the image from *redis:3.0.5* to *redis:3.0.6* (e.g. upgrade):

```
$ swarmctl service update redis --image redis:3.0.6
dy0vwwae8ropb5skvzcekzk6o

$ swarmctl service inspect redis
ID                   : dy0vwwae8ropb5skvzcekzk6o
Name                 : redis
Replicas             : 0/6
Update Status
 State               : UPDATING
 Started             : 28 seconds ago
 Message             : update in progress
Template
 Container
  Image              : redis:3.0.6

Task ID                      Service    Slot    Image          Desired State    Last State                  Node
-------                      -------    ----    -----          -------------    ----------                  ----
1v7x9wc383ut37dw4cwqkz9v4    redis      1       redis:3.0.6    RUNNING          PREPARING 25 seconds ago    node-1
a1rw3a7vhz3y1jbhfnzd55oft    redis      2       redis:3.0.6    RUNNING          PREPARING 25 seconds ago    node-3
6bl8maqveqep7cbvfz5dmix8o    redis      3       redis:3.0.6    RUNNING          PREPARING 25 seconds ago    node-1
3jv0r7yctyz4edryz1gq5sw0c    redis      4       redis:3.0.6    RUNNING          PREPARING 25 seconds ago    node-2
139jz9jqvwu6i3z6akn3geebs    redis      5       redis:3.0.6    RUNNING          PREPARING 25 seconds ago    node-2
34bn0omf5cjkrn9flunh0eei7    redis      6       redis:3.0.6    RUNNING          PREPARING 25 seconds ago    node-3
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
ID                         Name    Membership  Status  Availability  Manager Status
--                         ----    ----------  ------  ------------  --------------
7cna6u5nqcmi7moe9qnjzrss5  node-1  ACCEPTED    READY   DRAIN         REACHABLE *
cr962qysr3xr6k3ed5a5qjsde  node-3  ACCEPTED    READY   ACTIVE
du359trx1fnyhztwwpjlpa9yl  node-2  ACCEPTED    READY   ACTIVE

$ swarmctl service inspect redis
ID                   : dy0vwwae8ropb5skvzcekzk6o
Name                 : redis
Replicas             : 6/6
Update Status
 State               : PAUSED
 Started             : 1 minute ago
 Message             : update paused due to failure or early termination of task 77k803vw9jvzk3z6hhwlk0rni
Template
 Container
  Image              : redis:3.0.7

Task ID                      Service    Slot    Image          Desired State    Last State                Node
-------                      -------    ----    -----          -------------    ----------                ----
1drjw9e0gndey4njhzfxtodl7    redis      1       redis:3.0.7    RUNNING          RUNNING 12 seconds ago    node-2
a1rw3a7vhz3y1jbhfnzd55oft    redis      2       redis:3.0.6    RUNNING          RUNNING 1 minute ago      node-3
103z9iwdsu64z8ynra2bbq0h3    redis      3       redis:3.0.7    RUNNING          RUNNING 12 seconds ago    node-3
3jv0r7yctyz4edryz1gq5sw0c    redis      4       redis:3.0.6    RUNNING          RUNNING 1 minute ago      node-2
2iuu2da63y2wmbq1u0uok3p1b    redis      5       redis:3.0.7    RUNNING          RUNNING 12 seconds ago    node-2
34bn0omf5cjkrn9flunh0eei7    redis      6       redis:3.0.6    RUNNING          RUNNING 1 minute ago      node-3
```

As you can see, every Task running on `node-1` was rebalanced to either `node-2` or `node-3` by the reconciliation loop.
