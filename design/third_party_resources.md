# Third Party Resources

  * [Abstract](#abstract)
  * [Motivation](#motivation)
  * [Use Cases](#use-cases)
  * [Related Issues](#related-issues)
  * [Objectives](#objectives)
  * [Non-Objectives](#non-objectives)
  * [Proposed Changes](#proposed-changes)

##  Abstract

This document describes the a solution to managing accountable node level
resources unknown to docker swarm.

## Motivation

Each node is different in its own way, some nodes might have access to
accelerators, some nodes might have access to network devices and others
might support AVX while others only support SSE.
Swarmkit needs some simple way to account for these resources without having
to implement them each time a new kind of resource comes into existence.

While it is true that some resources can be advertised with labels, many
resources have a shareable capacity and can’t be represented well as a label.

The implementation we chose is to reuse a proven solution used by industry
projects (mesos and kubernetes) which lead us to implement two kinds
of Third Party Resources:
F
  * Integer TPR
  * Set TPR

Integier resources are for use cases where only an unsigned is needed to account
for the resource (see Linux Realtime).

A set would mostly be used for every resource which would need an
exclusive access to it.

## Constraints and Assumptions
1. Future work might require new mechanisms to be made to allow TPR to be cluster
wide in order to satisfy other use cases (e.g: pool of licenses)

## Use Cases

  * Exclusive access to discrete accelerators:
    * GPU devices
    * FPGA devices
    * MICs (Many-Integrated Core, such as Xeon Phi)
    * ...
  * Support for tracking additional cgroup quotas like cpu_rt_runtime.
    * [Linux Realtime](https://github.com/docker/docker/pull/23430)
  * PersistentDisks in GCE
  * Counting “slots” allowed access to a shared parallel file system.

## Related Issues

  * [Support abstract resource](https://github.com/docker/swarmkit/issues/594)
  * [Add new node filter to scheduler](https://github.com/docker/swarm/issues/2223)
  * [Add support for devices](https://github.com/docker/swarmkit/issues/1244)
  * [Resource Control](https://github.com/docker/swarmkit/issues/211)
  * [NVIDIA GPU support](https://github.com/docker/docker/issues/23917)
  * [Does Docker have plan to support allocating GPU](https://github.com/docker/docker/issues/24582)
  * [Docker Swarm to orchestrate "Swarm Cluster" which supports GPU](https://github.com/docker/docker/issues/24750)
  * [Use accelerator in docker container](https://github.com/docker/docker/issues/28642)
  * [Specify resource selectors](https://github.com/docker/swarmkit/issues/206)

## Objectives

1. Associate multiple TPRs with a node
2. Request some portion of available TPRs in the service
   during service creation
3. Enable users to define and schedule TPRs in a vanilla swarmkit cluster

## Non-Objectives

1. Solve how TPRs allocations are to be enforced or isolated.
2. Solve how TPRs are discovered
3. Solve how cluster-level TPRs should be advertised

## Proposed Changes

### Third Party Resource request

The services may only ask for TPRs as integers as the solution for asking for
specific resources can be solved in many different ways (filters, multiple kinds
of resources, ...) and should not be addressed in this PR.

```
$ # Single resource
$ swarmctl service create --name nginx --image nginx:latest --third-party-resource "banana=2"
$ # Multiple resources
$ swarmctl service create --name nginx --image nginx:latest --third-party-resource "banana=2;apple=3"
```

### Third Party Resource advertising

A node may advertise either an Integer number of resources or a set of resources.
It is the scheduler's job to decide which resource to assign and keep track of which task
owns which resource.

```
$ swarmd -d $DIR --join-addr $IP --join-token $TOKEN --third-party-resource "banana=blue,red,green;apple=8"
```

### Third Party Resource communication

As swarmkit is not responsible for the allocation of resources, it needs a way
to communicate how many TPRs were assigned (in the case of Integers) and what
resources were selected (in the case of sets).

The way we chose to implement this is through environment variables as they seem
to be the most straightforward way to advertise to the running container this.

**If we run `swarmctl inspect` we can see:**

```bash
$ swarmctl node inspect node-with-tpr
ID                        : 9toi8u8zo1qbkiw1d1nrsevdd
Hostname                  : node-3
Status:
  State                   : READY
  Availability            : ACTIVE
  Address                 : 127.0.0.1
Platform:
  Operating System        : linux
  Architecture            : x86_64
Resources:
  CPUs                    : 12
  Memory                  : 31 GiB
  apple                   : 3
  banana                  : red, blue, green
Plugins:
  Network                 : [bridge host macvlan null overlay]
  Volume                  : [local nvidia-docker]
Engine Version            : 1.13.1

$ swarmctl service create --name nginx --image nginx:latest --third-party-resource "banana=2;apple=2"
$ swarmctl service inspect nginx
ID                       : abxelhl822d8zyjqam3m3szb0
Name                     : nginx
Replicas                 : 1/1
Template
 Container
  Image                  : nginx:latest
  Resources
    Reservations:
      banana             : 2
      apple              : 2

Task ID                      Service    Slot    Image           Desired State    Last State                Node
-------                      -------    ----    -----           -------------    ----------                ----
6pbwd5qj7i0nsxlyi803qpf2x    nginx     1       nginx:latest    RUNNING          RUNNING 12 seconds ago    node-with-tpr

```
