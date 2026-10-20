<script lang="ts">
  export let kind: 'default' | 'primary' | 'success' | 'warning' | 'danger' | 'info' | 'purple' | 'inverse' = 'default';
  export let size: 'small' | 'medium' | 'large' = 'medium';
</script>

<span class="label {kind} {size}">
  <slot />
</span>

<style>
  .label {
    display: inline-block;
    margin: 2px;
    border: 1px solid transparent;
    border-radius: 2px;
    color: var(--white);
    text-align: center;
    white-space: nowrap;
    line-height: 1;
    vertical-align: middle;
  }

  /* Sizes */
  .small {
    padding: 1px 3px;
    font-size: 11px;
  }

  .medium {
    padding: 2px 5px;
    font-size: 12px;
  }

  .large {
    padding: 3px 7px;
    font-weight: bold;
    font-size: 14px;
  }

  /* Kinds */
  .default {
    background-color: var(--themeLightColor);
    border-color: var(--themeLightColor);
    color: var(--white);
  }

  .primary {
    background-color: var(--primaryBackgroundColor);
    border-color: var(--primaryBorderColor);
    color: var(--white);
  }

  .success {
    background-color: var(--successColor);
    border-color: var(--successColor);
    color: var(--white);
  }

  .warning {
    background-color: var(--warningColor);
    border-color: var(--warningColor);
    color: var(--white);
  }

  .danger {
    background-color: var(--dangerColor);
    border-color: var(--dangerColor);
    color: var(--white);
  }

  .info {
    background-color: var(--infoColor);
    border-color: var(--infoColor);
    color: var(--white);
  }

  .purple {
    background-color: var(--purple);
    border-color: var(--purple);
    color: var(--white);
  }

  .inverse {
    background-color: var(--inverseLabelColor);
    border-color: var(--inverseLabelColor);
    color: var(--inverseLabelTextColor);
  }
</style>
