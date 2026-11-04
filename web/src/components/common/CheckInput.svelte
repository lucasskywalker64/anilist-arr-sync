<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  export let checked = false;
  export let disabled = false;
  export let ariaLabel = 'Checkbox';

  const dispatch = createEventDispatcher<{ change: boolean }>();

  function toggle() {
    if (disabled) return;
    checked = !checked;
    dispatch('change', checked);
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === ' ' || e.key === 'Enter') {
      e.preventDefault();
      toggle();
    }
  }
</script>

<div
  role="checkbox"
  aria-checked={checked}
  aria-disabled={disabled}
  aria-label={ariaLabel}
  tabindex={disabled ? -1 : 0}
  class="checkInput {checked ? 'primaryIsChecked' : ''} {disabled ? 'disabled' : ''}"
  on:click={toggle}
  on:keydown={onKeyDown}
>
  {#if checked}
    <svg viewBox="0 0 24 24" width="14" height="14" stroke="currentColor" stroke-width="3" fill="none" stroke-linecap="round" stroke-linejoin="round">
      <polyline points="20 6 9 17 4 12"></polyline>
    </svg>
  {/if}
</div>

<style>
  .checkInput {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
    flex-shrink: 0;
    margin-top: 7px;
    margin-right: 5px;
    width: 20px;
    height: 20px;
    min-width: 20px;
    min-height: 20px;
    max-width: 20px;
    max-height: 20px;
    border: 1px solid #ccc;
    border-radius: 2px;
    background-color: var(--white);
    color: var(--white);
    text-align: center;
    line-height: 20px;
    cursor: pointer;
    vertical-align: middle;
    user-select: none;
    transition: background-color 150ms ease, border-color 150ms ease;
  }

  .checkInput:focus-visible {
    outline: 2px solid var(--sonarrBlue);
    outline-offset: 1px;
  }

  .primaryIsChecked {
    border-color: var(--primaryColor);
    background-color: var(--primaryColor);
    color: var(--white);
  }

  .disabled {
    background-color: var(--disabledCheckInputColor);
    cursor: not-allowed;
    opacity: 0.6;
  }
</style>
