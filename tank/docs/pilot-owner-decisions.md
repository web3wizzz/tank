# Hosted pilot owner decisions — draft

**No option below has been approved.** Resolve these items with the host and
pilot-user owners before inviting external users. Do not imply that the current
local process tests or public registry transaction provide a retention promise.

The [independent-host handoff checklist](independent-host-pilot-handoff.md#decisions-the-owner-must-supply)
also lists the required host inventory, origins, access, custody handover,
workload, monitoring and backup inputs. Supply authentication through private local
files; these documents contain no approved hosts or access credentials.

| Decision | Options to review | Trade-off to record |
| --- | --- | --- |
| Retention | Fixed end date; renewable term; or export-only window after a named pilot close | Specify dates, renewal notice, and what happens to shards and metadata when time expires. |
| Funding | Named operator funds service costs; a time-limited grant covers them; or pilot scope pauses when the funded period ends | Identify who pays for hosts, bandwidth, backups, monitoring, and repair. Real-fund or paid infrastructure needs separate explicit authorization. |
| Operator exit | Transfer service and custody to a named successor; offer a funded handover window; or stop accepting uploads and provide an export period | Name the successor, notice period, shard-transfer plan, and remaining funding. Do not assume a replacement exists. |
| Capacity or outage | Pause new uploads; reduce limits; or prioritize recovery for already accepted files | Define who makes the call, incident contacts, and communication timing. |
| Pilot close | User export followed by deletion on an owner-selected schedule; or an explicitly funded extension | Set notice and export dates, backup expiry, and separate shard/database deletion steps. No automatic deletion is currently enabled. |
| User participation | Invite-only synthetic evaluation; or a later opt-in pilot with written terms | Identify eligible users, support scope, data classification, and the independent-copy requirement. |

The initial architecture assumes three active storage hosts and one spare. Record
provider, region, physical failure domain, custody owner, and monitoring contact
in the private operator inventory. Three endpoints or containers on one machine
do not satisfy that assumption.
