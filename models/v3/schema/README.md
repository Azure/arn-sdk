# Schema High Level

# Introduction

The schema is a one we inherit from ARN.  The original version of this SDK did autorest to generate the types, but this left a lot of bad types in here.

Specifically a lot of pointers to basic types (which kill the garbage collector), pointers to structs that weren't needed, a lot of interfaces that should have been basic types.

If you are the SDK team, you may need to resort to this because there are too many version changes to keep up with by hand. Unfortunately, instead of having something like proto where you can make fixes to these things in a single place (the proto representation), this all comes from swagger.  

I made the decision to leave autorest because it will be better for all users and I expect ARN team to keep these definitions up to date. Changes should be minor or one time changes when you get a new schema version (which will probably just be added fields).

# Relevant documents

* V3 and V5 schemas design: https://microsoft.sharepoint.com/:w:/t/GovernanceVteam/EV8drAcjfklDpmj98eVdsc0B8uruJDoB_ewXOv_CapG3Fg?e=YjIG9r&wdOrigin=TEAMS-MAGLEV.p2p_ns.rwc&wdExp=TEAMS-TREATMENT&wdhostclicktime=1718925637876&web=1
* V3 schema: https://eng.ms/docs/cloud-ai-platform/azure-core/azure-management-and-platforms/control-plane-bburns/azure-resource-notifications/azure-resource-notifications-documentation/partners/arn-schema/arn-schema-v3
* The ARM resource definition: https://msazure.visualstudio.com/One/_git/Mgmt-Governance-Notifications?path=%2Fsrc%2FLibraries%2FARNContracts%2FResourceContracts%2FGenericResource.cs
