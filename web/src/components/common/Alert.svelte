<script lang="ts">
  export let kind: 'danger' | 'info' | 'success' | 'warning' = 'info';
</script>

<div class="alert {kind}" role="alert">
  <slot />
</div>

<style>
  .alert {
    display: block;
    margin: 10px 0;
    padding: 15px 20px;
    border: 1px solid transparent;
    border-radius: 4px;
    line-height: 1.5;
  }

  .danger {
    border-color: var(--alertDangerBorderColor);
    background-color: var(--alertDangerBackgroundColor);
    color: var(--alertDangerColor);
  }

  .info {
    border-color: var(--alertInfoBorderColor);
    background-color: var(--alertInfoBackgroundColor);
    color: var(--alertInfoColor);
  }

  .success {
    border-color: var(--alertSuccessBorderColor);
    background-color: var(--alertSuccessBackgroundColor);
    color: var(--alertSuccessColor);
  }

  .warning {
    border-color: var(--alertWarningBorderColor);
    background-color: var(--alertWarningBackgroundColor);
    color: var(--alertWarningColor);
  }
</style>
