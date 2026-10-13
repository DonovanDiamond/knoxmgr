<script lang="ts">
	let {
		id,
		label,
		value = $bindable(''),
		type = 'text',
		error = '',
		hint = '',
		readonly = false,
		autocomplete = 'off',
		inputmode,
		oninput
	}: {
		id: string;
		label: string;
		value?: string;
		type?: 'text' | 'email' | 'tel' | 'password';
		/** Shown in place of the hint and marks the input invalid. */
		error?: string;
		hint?: string;
		readonly?: boolean;
		autocomplete?: AutoFill;
		inputmode?: 'text' | 'tel' | 'email' | 'numeric';
		/** Called with the new value after the user types. */
		oninput?: (value: string) => void;
	} = $props();
</script>

<div>
	<label class="lbl" for={id}>{label}</label>
	<input
		{id}
		{type}
		{readonly}
		{autocomplete}
		{inputmode}
		class="field"
		bind:value
		oninput={(e) => oninput?.(e.currentTarget.value)}
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={error || hint ? `${id}-msg` : undefined}
	/>
	{#if error}
		<p id="{id}-msg" class="err">{error}</p>
	{:else if hint}
		<p id="{id}-msg" class="hint">{hint}</p>
	{/if}
</div>
