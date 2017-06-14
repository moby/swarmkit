# Meeting #1: June 13, 2017 @ 3pm PST 
 
## Attendees:
- Diogo Monica (Docker)
- Riyaz Faizullabhoy (Docker)
- Ying Li (Docker)
- Aaron Lehmann (Docker)
- Olivia Barnett (Docker)
- Lorenzo Fontana - fntlnz
 
## Agenda: 
- External Secrets Design Doc review.
- Service Identities Design Doc review.

## PRs:
- External swarmkit secrets PR review #2239 
- [ca/node] Maybe update the root CA when renewing the TLS cert  #2238
 
## Notes:
- Welcome to SIG! (“Special Interest Group”)
- Three topic focuses:
  - secrets management and how to integrate with external secrets management systems
  - service identities: certificates issued by orchestrator to services
  - entitlements: providing capability and privilege to services
- Administrivia:
  - Weekly meeting
  - Finding a good meeting time - feedback welcome
  - Can propose discussion items to Agenda section and PRs to review on PRs section: will be approved by SIG moderator for meeting
  - All interactions in the open, with notes!
- External Secrets: pluggable way of having external developers contribute implementations to store secrets long-term on external systems, have swarm be “last-mile” delivery
  - Plugin model:
    - Swarm-wide plugins aren’t a reality yet - need plugins to only run in managers and have configuration management. Brian Goff working on plugins https://github.com/moby/moby/pull/33575 , Liron working on first implementation of external plugin https://github.com/docker/swarmkit/pull/2239  - please help them!
  - No orchestrator code should get modified for a plugin
  - Goal is to have security guarantees from Swarm secrets should extend to external implementations
  - Vault implementation coming soon with help from CyberArk
  - Plugins can leverage swarm secrets to secure connections
- Lorenzo: how can we be sure that secrets doesn’t leak around network / memory while allowing external secrets?  Is this up to the plugin? using swarm secrets to bootstrap secure network connections in the plugins (everything should be encrypted on the network)
plugins should only be enabled on managers (least privilege)
- Consider ramfs vs. tmpfs, with size restrictions (mitigating swap)
- Diogo: service identities implementations could even be an external plugin. Swarm manager could recognize the plugin type and send over the appropriate keypair to sign as a CA for new service identities
    - Pro: allows for users to create service identity certs their own way
    - Con: not available by default, stacked on top of secrets and plugins so work cannot be parallelized
- Discussion: worth considering, TODO: create design doc
- Service identities ultimate goal: implement something like istio on top of Swarm to run 3rd party solutions while keeping security guarantees of swarm. Reduce number of overlay networks to 1
- Goal: should work with Istio and k8s community, have a Docker istio/k8s maintainer
- Goal: seamlessly integrate with them
- Goal: open decision making in community

## Next steps:
- please review external secrets PR https://github.com/docker/swarmkit/pull/2239 
- please review documents
- discuss on community slack #orchestration-sec
- Goal: codefreeze for 17.08 plugin, 17.09 for fully featured plugin - this should get shipped when it’s ready and thoroughly tested
- Istio: https://github.com/istio/istio
  - Will invite folks from Istio next week or week after to present
