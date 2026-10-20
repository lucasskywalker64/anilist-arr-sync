<script lang="ts">
  export let kind: 'default' | 'primary' | 'success' | 'warning' | 'danger' = 'default';
  export let size: 'small' | 'medium' | 'large' = 'medium';
  export let type: 'button' | 'submit' | 'reset' = 'button';
  export let disabled = false;
</script>

<button
  {type}
  class="btn {kind} {size}"
  {disabled}
  on:click
>
  <slot />
</button>

<style>
  .btn {
    border: 1px solid;
    border-radius: 4px;
    vertical-align: middle;
    text-align: center;
    white-space: nowrap;
    line-height: normal;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-family: inherit;
    transition: background-color 150ms ease, border-color 150ms ease;
  }

  .btn:disabled {
    opacity: 0.65;
    cursor: not-allowed;
  }

  /* Sizes */
  .small {
    padding: 1px 5px;
    font-size: 12px;
    height: 24px;
  }

  .medium {
    padding: 6px 16px;
    font-size: 14px;
    height: 35px;
  }

  .large {
    padding: 10px 20px;
    font-size: 16px;
    height: 42px;
  }

  /* Kinds */
  .default {
    background-color: var(--defaultBackgroundColor);
    border-color: var(--defaultBorderColor);
    color: var(--defaultButtonTextColor);
  }
  .default:hover:not(:disabled) {
    background-color: var(--defaultHoverBackgroundColor);
    border-color: var(--defaultHoverBorderColor);
  }

  .primary {
    background-color: var(--primaryBackgroundColor);
    border-color: var(--primaryBorderColor);
    color: var(--white);
  }
  .primary:hover:not(:disabled) {
    background-color: var(--primaryHoverBackgroundColor);
    border-color: var(--primaryHoverBorderColor);
  }

  .success {
    background-color: var(--successBackgroundColor);
    border-color: var(--successBorderColor);
    color: var(--white);
  }
  .success:hover:not(:disabled) {
    background-color: var(--successHoverBackgroundColor);
    border-color: var(--successHoverBorderColor);
  }

  .warning {
    background-color: var(--warningBackgroundColor);
    border-color: var(--warningBorderColor);
    color: var(--white);
  }
  .warning:hover:not(:disabled) {
    background-color: var(--warningHoverBackgroundColor);
    border-color: var(--warningHoverBorderColor);
  }

  .danger {
    background-color: var(--dangerBackgroundColor);
    border-color: var(--dangerBorderColor);
    color: var(--white);
  }
  .danger:hover:not(:disabled) {
    background-color: var(--dangerHoverBackgroundColor);
    border-color: var(--dangerHoverBorderColor);
  }
</style>
