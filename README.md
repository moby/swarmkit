# Swarm: Cluster orchestration for Docker

[![GoDoc](https://godoc.org/github.com/docker/swarm-v2?status.png)](https://godoc.org/github.com/docker/swarm-v2)
[![Circle CI](https://circleci.com/gh/docker/swarm-v2.svg?style=shield&circle-token=a7bf494e28963703a59de71cf19b73ad546058a7)](https://circleci.com/gh/docker/swarm-v2)
[![codecov.io](https://codecov.io/github/docker/swarm-v2/coverage.svg?branch=master&token=LqD1dzTjsN)](https://codecov.io/github/docker/swarm-v2?branch=master)

## Build

Requirements:

- go 1.6
- A [working golang](https://golang.org/doc/code.html) environment

From the project root directory, run:
```
make binaries
```

Because this project's code continues to evolve rapidly, you should rebuild from master regularly. Any git tutorial can help you, but in general terms you will:

```sh
$ cd $GOPATH/src/github.com/docker/swarm-v2
$ git checkout master
$ git pull origin master
$ make binaries
```

## Install

```sh
$ sudo -E PATH=$PATH make install
```

This will install `/usr/local/bin/swarmd` (the manager and agent) and `/usr/local/bin/swarmctl` (the command line tool).

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

**1 manager + 2 agent cluster on a single host**

These instructions assume that `swarmd` and `swarmctl` are in your PATH.

(Before starting, make sure `/tmp/managerN` and `/tmp/agentN` don't exist for any N.)

Start the manager:

```sh
$ swarmd manager -l info --state-dir /tmp/manager1
```

In two additional terminals, start two agents:

```sh
$ swarmd agent -l info --hostname node-1 -d /tmp/agent1
$ swarmd agent -l info --hostname node-2 -d /tmp/agent2
```

In a fourth terminal, use `swarmctl` to explore and control the cluster.  List nodes:

```
$ swarmctl node ls
ID                         Name      Status  Availability
87pn3pug404xs4x86b5nwlwbr  node-1    READY   ACTIVE
by2ihzjyg9m674j3cjdit3reo  node-2    READY   ACTIVE
```

**Create and manage an application**

Start with the `hello-world` application in `examples/hello-world.yml`. `hello-world` consists of 1 service: `ping`.

```
$ cd examples/
$ cat hello-world.yml
version: '3'
namespace: hello-world

services:
  ping:
      image: alpine
      command: ["sh", "-c", "ping $HOST"]
      env:
          - HOST=google.com
      instances: 2
```

Start 'hello-world':

```
$ swarmctl deploy -f hello-world.yml
ping: dazkblnyzh46hziagcrffgkkz - CREATED
```

List the running services:

```
$ swarmctl service ls
ID                         Name  Image   Instances
chlkcf9v19kxbccspmiyuttgz  ping  alpine  2
```

Inspect the service:

```
$ swarmctl service inspect ping
ID                : 3ud29r25fop28v6ryznhwv0j6
Name              : ping
Instances         : 5
Strategy          : SERVICE_STRATEGY_SPREAD
Template
 Container
  Image           : alpine
  Command         : "sh -c ping $HOST"
  Env             : [HOST=google.com]

Task ID                      Instance    Image     Desired State    Last State                Node
-------                      --------    -----     -------------    ----------                ----
50lq7imo6h5q1pxur5odm2n7j    ping.1      alpine    RUNNING          RUNNING 20 seconds ago    node-1
7mveed4qn5iqtx44jyfabk5os    ping.2      alpine    RUNNING          RUNNING 20 seconds ago    node-2
```

Now change instance count in the YAML file:

```
$ vi hello-world.yml
[change instances to 3 and save]
```

Let's look at the delta:

```sh
$ swarmctl diff -f hello-world.yml
--- remote
+++ local
@@ -10,7 +10,7 @@
     env:
     - HOST=google.com
     name: ping
-    instances: 2
+    instances: 3
     mode: replicated
     restart: always
     restartdelay: "0"
```

Redeploy the service with the modified manifest and see the result:

```sh
$ swarmctl deploy -f hello-world.yml
ping: d73nsz5tzhflxlu9v23pfb5si - UPDATED
$
$ swarmctl service ls
ID                         Name  Image   Instances
chlkcf9v19kxbccspmiyuttgz  ping  alpine  3
```

You can also update instance count on the command line with `--instances`:

```
$ swarmctl service update ping --instances 4
chlkcf9v19kxbccspmiyuttgz
$
$ swarmctl service inspect ping
ID                : 3ud29r25fop28v6ryznhwv0j6
Name              : ping
Instances         : 5
Strategy          : SERVICE_STRATEGY_SPREAD
Template
 Container
  Image           : alpine
  Command         : "sh -c ping $HOST"
  Env             : [HOST=google.com]

Task ID                      Instance    Image     Desired State    Last State                Node
-------                      --------    -----     -------------    ----------                ----
50lq7imo6h5q1pxur5odm2n7j    ping.1      alpine    RUNNING          RUNNING 8 minutes ago     node-1
7mveed4qn5iqtx44jyfabk5os    ping.2      alpine    RUNNING          RUNNING 8 minutes ago     node-2
9aiegisquaggywa0o3w1syksb    ping.3      alpine    RUNNING          RUNNING 6 minutes ago     node-2
erly2j4b8hygo2ag8sevm2zop    ping.4      alpine    RUNNING          RUNNING 26 seconds ago    node-1
```

You can also live edit the state file on the manager:

```
$ EDITOR=nano swarmctl service edit ping
[change instances to 5, Ctrl+o to save, Ctrl+x to exit]
--- old
+++ new
@@ -6,7 +6,7 @@
 env:
 - HOST=google.com
 name: ping
-instances: 4
+instances: 5
 mode: replicated
 restart: always
 restartdelay: "0"
Apply changes? [N/y]

Apply changes? [N/y] y
chlkcf9v19kxbccspmiyuttgz
```

Now check the result:

```sh
$ swarmctl service ls
ID                         Name  Image   Instances
chlkcf9v19kxbccspmiyuttgz  ping  alpine  5
$ swarmctl service inspect ping
ID                : 3ud29r25fop28v6ryznhwv0j6
Name              : ping
Instances         : 5
Strategy          : SERVICE_STRATEGY_SPREAD
Template
 Container
  Image           : alpine
  Command         : "sh -c ping $HOST"
  Env             : [HOST=google.com]

Task ID                      Instance    Image     Desired State    Last State                Node
-------                      --------    -----     -------------    ----------                ----
50lq7imo6h5q1pxur5odm2n7j    ping.1      alpine    RUNNING          RUNNING 10 minutes ago    node-1
7mveed4qn5iqtx44jyfabk5os    ping.2      alpine    RUNNING          RUNNING 10 minutes ago    node-2
9aiegisquaggywa0o3w1syksb    ping.3      alpine    RUNNING          RUNNING 8 minutes ago     node-2
erly2j4b8hygo2ag8sevm2zop    ping.4      alpine    RUNNING          RUNNING 2 minutes ago     node-1
3f4peh8qiykhxrbswildn2qlr    ping.5      alpine    RUNNING          RUNNING 31 seconds ago    node-1
```

## Multiple Managers

You can setup multiple managers. To initiate the first manager:

```
$ swarmd manager --state-dir "/tmp/manager1" --listen-addr "0.0.0.0:4242"
```

To start additional managers:

```
$ swarmd manager --state-dir "/tmp/manager2" --listen-addr "0.0.0.0:4243" --join-cluster "0.0.0.0:4242"
$ swarmd manager --state-dir "/tmp/manager3" --listen-addr "0.0.0.0:4244" --join-cluster "0.0.0.0:4242"
[...]
```

To add a new manager, first start a new manager process. The manager will request a certificate, and this request must be approved
before the manager can join the Raft cluster. Use `swarmctl cert ls` to view pending requests:

```
$ swarmctl cert ls
ID                         CN                         Role           Status
--                         --                         ----           ------
643nz8xh4gwelv16x8262wumq  b9jbgg4mzhbepi7kxcb2gnitg  swarm-manager  ISSUANCE_PENDING
```

Then, accept the new manager using:

```
$ swarmctl cert accept 643nz8xh4gwelv16x8262wumq
```

You can bypass the need for manual certificate approval by turning on automatic certificate issuance using this command:

```
$ swarmctl cluster update default --autoaccept agent,manager
```
