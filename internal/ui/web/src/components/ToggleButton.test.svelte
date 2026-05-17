<script lang="ts">
  import ToggleButton from './ToggleButton.svelte';

  interface Props {
    label?: string;
    on?: boolean;
    failing?: boolean;
    loading?: boolean;
    disabled?: boolean;
    title?: string;
    onclick?: (e: MouseEvent) => void;
  }
  let { label = 'HTTPS', on = false, failing = false, loading = false, disabled = false, title = '', onclick }: Props = $props();
</script>

<ToggleButton {label} {on} {failing} {loading} {disabled} {title} {onclick} />
