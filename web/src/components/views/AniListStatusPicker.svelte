<script context="module" lang="ts">
  const statuses = [
    { value: 'CURRENT', label: 'Watching' },
    { value: 'PLANNING', label: 'Planning' },
    { value: 'COMPLETED', label: 'Completed' },
    { value: 'PAUSED', label: 'Paused' },
    { value: 'REPEATING', label: 'Rewatching' },
    { value: 'DROPPED', label: 'Dropped' },
  ];

  function selectedStatuses(value: string | undefined) {
    const configured = value?.toUpperCase().split(',').map(status => status.trim());
    const selected = statuses.filter(status => configured?.includes(status.value));
    return selected.length ? selected : statuses.slice(0, 1);
  }

  export function describeListStatuses(value: string | undefined) {
    return selectedStatuses(value).map(status => status.label).join(', ');
  }
</script>

<script lang="ts">
  export let label: string;
  export let value: string | undefined = undefined;

  $: selected = selectedStatuses(value);

  function toggle(status: typeof statuses[number], checked: boolean) {
    value = statuses.filter(option => option === status ? checked : selected.includes(option))
      .map(option => option.value).join(',');
  }
</script>

<fieldset>
  <legend>{label}</legend>
  <div class="options">
    {#each statuses as status (status.value)}
      {@const checked = selected.includes(status)}
      <label class="checkbox">
        <input type="checkbox" value={status.value} {checked} disabled={checked && selected.length === 1}
          on:change={event => toggle(status, event.currentTarget.checked)} />
        {status.label}
      </label>
    {/each}
  </div>
</fieldset>

<style>
  fieldset { border: 0; margin: 16px 0; padding: 0; min-width: 0; }
  legend { font-weight: 600; margin-bottom: 8px; padding: 0; }
  .options { display: flex; flex-wrap: wrap; gap: 8px 16px; }
  label { display: flex; align-items: center; gap: 6px; }
</style>
