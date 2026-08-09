import { render, fireEvent } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import ServicePortsTab from './ServicePortsTab.svelte';
import type { Service } from '$stores/services';

const { setServicePorts } = vi.hoisted(() => ({ setServicePorts: vi.fn(async () => ({ ok: true })) }));
vi.mock('$stores/services', async (orig) => {
  const actual = (await orig()) as object;
  return { ...actual, setServicePorts };
});

function svc(over: Partial<Service> = {}): Service {
  return {
    name: 'rustfs',
    status: 'active',
    site_count: 0,
    preset_owned: true,
    default_port: 9000,
    secondary_ports: [{ container: 9001, default: 9001 }],
    ...over
  } as Service;
}

describe('ServicePortsTab', () => {
  beforeEach(() => setServicePorts.mockClear());

  it('saves a changed primary port', async () => {
    const { container, getByText } = render(ServicePortsTab, { props: { svc: svc() } });
    const input = container.querySelector('input[type="number"]') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: '9010' } });
    await fireEvent.click(getByText('Save'));
    expect(setServicePorts).toHaveBeenCalledWith('rustfs', {
      published_port: 9010,
      published_ports: { '9001': 9001 },
      extra_ports: []
    });
  });

  it('clears a secondary override when its field is blanked', async () => {
    const s = svc({ secondary_ports: [{ container: 9001, default: 9001, published: 39002 }] });
    const { container, getByText } = render(ServicePortsTab, { props: { svc: s } });
    const inputs = container.querySelectorAll('input[type="number"]');
    // Blank the seeded 39002 override: an empty field means reset to default, so
    // the default is sent and the backend clears the override.
    await fireEvent.input(inputs[1], { target: { value: '' } });
    await fireEvent.click(getByText('Save'));
    expect(setServicePorts).toHaveBeenCalledWith('rustfs', {
      published_port: null,
      published_ports: { '9001': 9001 },
      extra_ports: []
    });
  });

  it('saves a changed secondary port keyed by container port', async () => {
    const { container, getByText } = render(ServicePortsTab, { props: { svc: svc() } });
    const inputs = container.querySelectorAll('input[type="number"]');
    // Second published-port field is the 9001 secondary mapping.
    await fireEvent.input(inputs[1], { target: { value: '9002' } });
    await fireEvent.click(getByText('Save'));
    expect(setServicePorts).toHaveBeenCalledWith('rustfs', {
      published_port: null,
      published_ports: { '9001': 9002 },
      extra_ports: []
    });
  });

  it('shows the save action only once a field is edited', async () => {
    const { container, queryByText } = render(ServicePortsTab, { props: { svc: svc() } });
    expect(queryByText('Save')).toBeNull();
    const input = container.querySelector('input[type="number"]') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: '9010' } });
    expect(queryByText('Save')).not.toBeNull();
  });
});
