import { PostgresConfig, SSHConfig } from '@/grpc_generated/peers';
import { Dispatch, SetStateAction } from 'react';
import { PeerSetting } from './common';

export const postgresSetting: PeerSetting[] = [
  {
    label: 'Host',
    stateHandler: (value, setter) =>
      setter((curr) => ({ ...curr, host: value })),
    tips: 'Specifies the IP host name or address on which postgres is to listen for TCP/IP connections from client applications. Ensure that this host has us whitelisted so we can connect to it.',
  },
  {
    label: 'Port',
    stateHandler: (value, setter) =>
      setter((curr) => ({ ...curr, port: parseInt(value, 10) })),
    type: 'number', // type for textfield
    default: 5432,
    tips: 'Specifies the TCP/IP port or local Unix domain socket file extension on which postgres is listening for connections from client applications.',
  },
  {
    label: 'User',
    stateHandler: (value, setter) =>
      setter((curr) => ({ ...curr, user: value })),
    tips: 'Specify the user that we should use to connect to this host.',
    helpfulLink: 'https://www.postgresql.org/docs/8.0/user-manag.html',
  },
  {
    label: 'Password',
    stateHandler: (value, setter) =>
      setter((curr) => ({ ...curr, password: value })),
    type: 'password',
    tips: 'Password associated with the user you provided.',
    helpfulLink: 'https://www.postgresql.org/docs/current/auth-password.html',
  },
  {
    label: 'Database',
    stateHandler: (value, setter) =>
      setter((curr) => ({ ...curr, database: value })),
    tips: 'Specify which database to associate with this peer.',
    helpfulLink:
      'https://www.postgresql.org/docs/current/sql-createdatabase.html',
  },
  {
    label: 'Transaction Snapshot',
    stateHandler: (value, setter) =>
      setter((curr) => ({ ...curr, transactionSnapshot: value })),
    optional: true,
    tips: 'This is optional and only needed if this peer is part of any query replication mirror.',
  },
];

type sshSetter = Dispatch<SetStateAction<SSHConfig>>;
export const sshSetting = [
  {
    label: 'Host',
    stateHandler: (value: string, setter: sshSetter) =>
      setter((curr: SSHConfig) => ({ ...curr, host: value })),
    tips: 'Specifies the IP host name or address of your instance.',
  },
  {
    label: 'Port',
    stateHandler: (value: string, setter: sshSetter) =>
      setter((curr) => ({ ...curr, port: parseInt(value, 10) })),
    type: 'number',
    default: 5432,
    tips: 'Specifies the TCP/IP port or local Unix domain socket file extension on which clients can connect.',
  },
  {
    label: 'User',
    stateHandler: (value: string, setter: sshSetter) =>
      setter((curr) => ({ ...curr, user: value })),
    tips: 'Specify the user that we should use to connect to this host.',
  },
  {
    label: 'Password',
    stateHandler: (value: string, setter: sshSetter) =>
      setter((curr) => ({ ...curr, password: value })),
    type: 'password',
    optional: true,
    tips: 'Password associated with the user you provided.',
  },
  {
    label: 'BASE64 Private Key',
    stateHandler: (value: string, setter: sshSetter) =>
      setter((curr) => ({ ...curr, privateKey: value })),
    optional: true,
    tips: 'Private key as a BASE64 string for authentication in order to SSH into your machine.',
  },
];

export const blankSSHConfig: SSHConfig = {
  host: '',
  port: 22,
  user: '',
  password: '',
  privateKey: '',
};

export const blankPostgresSetting: PostgresConfig = {
  host: '',
  port: 5432,
  user: '',
  password: '',
  database: '',
  transactionSnapshot: '',
};
