<script lang="ts">
  export let label = '';
  export let disabled = false;
  export let iconColor: string | undefined = undefined;
</script>

<button
  type="button"
  class="toolbarButton"
  {disabled}
  on:click
>
  <div class="iconContainer" style={iconColor ? `color: ${iconColor}` : ''}>
    <slot />
  </div>
  <div class="labelContainer">
    <span class="label">{label}</span>
  </div>
</button>

<style>
  .toolbarButton {
    padding-top: 4px;
    width: var(--toolbarButtonWidth);
    height: var(--toolbarHeight);
    text-align: center;
    cursor: pointer;
    color: var(--toolbarColor);
    background: transparent;
    border: none;
    display: inline-flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    transition: color 150ms ease;
    user-select: none;
  }

  .toolbarButton:hover:not(:disabled) {
    color: var(--toobarButtonHoverColor);
  }

  .toolbarButton:disabled {
    color: var(--disabledColor);
    cursor: default;
    opacity: 0.5;
  }

  .iconContainer {
    height: 21px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  :global(.iconContainer svg) {
    width: 20px;
    height: 20px;
  }

  .labelContainer {
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .label {
    font-size: var(--extraSmallFontSize);
    line-height: 12px;
    color: var(--toolbarLabelColor);
    padding: 0 3px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .toolbarButton:hover:not(:disabled) .label {
    color: var(--toobarButtonHoverColor);
  }
</style>
