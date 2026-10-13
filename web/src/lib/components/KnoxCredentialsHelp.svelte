<div class="note mb-5">
	<details class="w-full">
		<summary class="cursor-pointer font-semibold">How do I get these?</summary>
		<ol class="mt-3 list-decimal space-y-2 pl-5">
			<li>
				Sign in to the Knox Admin Portal (<a class="link" href="https://www.samsungknox.com" target="_blank" rel="noopener noreferrer">samsungknox.com</a>) and open Knox Manage. The browser address shows your region server, for example
				<code>https://eu02.manage.samsungknox.com</code>. That is the <strong>Region</strong> to choose (or the <strong>Base URL</strong> under Custom URL).
			</li>
			<li>In Knox Manage go to <strong>Advanced &gt; EMM API &gt; API Client</strong> and click <strong>Add</strong>.</li>
			<li>
				<strong>Client ID</strong>: any unique name (for example <code>knoxmgr</code>). <strong>Password</strong>: 8 to 30 characters with letters, numbers and special characters. This is the <strong>Client secret</strong>.
				<strong>Token Validity(sec)</strong>: up to 3600. <strong>API Permission Level</strong>: Management (Read Only works for viewing only; creating users, invitations and group changes need Management). Save.
			</li>
			<li>Make sure the client is <strong>Active</strong>. Only five API clients can be active; select it and click <strong>Change status</strong> if needed.</li>
			<li>
				<strong>Tenant ID</strong>: in the Knox Admin Portal, open the account icon menu (top right). The Knox Manage tenant ID is shown there, usually your organisation's domain, sometimes with a prefix.
			</li>
			<li>Enter the Client ID, Tenant ID and Client secret here, then click <strong>Test connection</strong>. knoxmgr joins them as <code>clientId@tenantId</code> when it signs in.</li>
		</ol>
		<p class="mt-3">
			<a
				class="link"
				href="https://docs.samsungknox.com/admin/knox-manage/original-console/appendix/manage-open-api/manage-api-clients/"
				target="_blank"
				rel="noopener noreferrer">Samsung docs: Manage API clients</a>
		</p>
	</details>
</div>
