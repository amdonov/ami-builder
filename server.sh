yum install -y epel-release
yum install -y ansible
mkdir -p group_vars
# Create ansible settings file
cat > group_vars/all << EOF
image: ami-adf1e9ba
domain: lin.gfclab.com
foreman: "foreman.{{ domain }}"
admin_password: password123
ds_password: password123
dns_forwarder: 10.208.86.2
realm: LIN.GFCLAB.COM
ansible_ssh_private_key_file: files/ansible.pem
ansible_ssh_user: booz-user
userdata: |
       #cloud-config
       hostname: {{ item.hostname }}
       fqdn: {{ item.fqdn }}
       manage_etc_hosts: true
       manage_resolv_conf: true
       resolv_conf:
         nameservers:
           - {{ dns_forwarder }}
         searchdomains:
           - {{ domain }}
         options:
           rotate: true
           timeout: 1
       puppet:
         install: false
         conf:
           agent:
             pluginsync: true
             report: true
             ignoreschedules: true
             daemon: false
             ca_server: {{ foreman }}
             certname: {{ item.fqdn }}
             environment: production
             server: {{ foreman }}
vms:
        - hostname: ipa1
          fqdn: "ipa1.{{ domain }}"
          tags:
           Name: "ipa1.{{ domain }}"
           type: ipa
           version: 7.3
          security_groups:
           - ssh
           - ipa
          public_ip: no
          instance_type: t2.micro
          role: ipa_master
          iam:
        - hostname: ipa2
          fqdn: "ipa2.{{ domain }}"
          tags:
           Name: "ipa2.{{ domain }}"
           type: ipa
           version: 7.3
          security_groups:
           - ssh
           - ipa
          public_ip: no
          instance_type: t2.micro
          role: ipa_replica
          iam:
        - hostname: foreman
          fqdn: "foreman.{{ domain }}"
          tags:
           Name: "foreman.{{ domain }}"
           type: foreman
           version: 7.3
          security_groups:
           - ssh
           - foreman
          public_ip: no
          instance_type: t2.medium
          role: foreman
          iam:
        - hostname: ansible
          fqdn: "ansible.{{ domain }}"
          tags:
           Name: "ansible.{{ domain }}"
           type: ansible
           version: 7.3
          security_groups:
           - ssh
           - prov-server
          public_ip: no
          instance_type: t2.micro
          role: ansible
          iam: ansible
        - hostname: jump
          fqdn: "jump.{{ domain }}"
          tags:
           Name: "jump.{{ domain }}"
           type: jump
           version: 7.3
          security_groups:
           - jump
          public_ip: no
          instance_type: t2.micro
          role: jump
          iam:                      
EOF

# Create DNS template
cat > resolv.conf.j2 << EOF
nameserver {{ ipaserver_ip }}
EOF

# Create Hammer template
cat > cli_config.yml.j2 << EOF
:modules:
    - hammer_cli_foreman

:foreman:
    :host: 'https://{{ fqdn }}/'
    :username: 'admin'
    :password: '{{ admin_password }}'

:log_dir: '~/.foreman/log'
:log_level: 'error'
EOF

# Create ansible playbook
cat > start.yml << EOF
---
- hosts: localhost
  connection: local
  tasks:
   - name: Get AWS Information
     ec2_facts:

   - name: Set Network Variables
     set_fact:
      vpc: "{{ facter_ec2_metadata.network.interfaces.macs[facter_ec2_metadata.mac]['vpc-id'] }}"
      cidr: "{{ facter_ec2_metadata.network.interfaces.macs[facter_ec2_metadata.mac]['vpc-ipv4-cidr-block'] }}"
      subnet: "{{ facter_ec2_metadata.network.interfaces.macs[facter_ec2_metadata.mac]['subnet-id'] }}"
      region: "{{ ansible_ec2_placement_region }}"

   - name: Create a new keypair
     ec2_key:
        name: ansible
        region: "{{ region }}"
     register: keypair

   - name: Save private key
     copy:
        dest: files/ansible.pem
        content: "{{ keypair.key.private_key }}"
        mode: 0600
     when: keypair.changed    
 
   - name: Create Jump Server Security Group
     ec2_group:
        name: jump
        description: Allow SSH from anywhere
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 22
            to_port: 22
            cidr_ip: 0.0.0.0/0

   - name: Create SSH Server Security Group
     ec2_group:
        name: ssh
        description: Allow SSH within VPC
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 22
            to_port: 22
            cidr_ip: "{{ cidr }}"

   - name: Create Provision Server Security Group
     ec2_group:
        name: prov-server
        description: Rules for Provisioning servers
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 8080
            to_port: 8080
            cidr_ip: "{{ cidr }}"

   - name: Create Foreman Server Security Group
     ec2_group:
        name: foreman
        description: Rules for Foreman servers
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 80
            to_port: 80
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 443
            to_port: 443
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 8443
            to_port: 8443
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 8140
            to_port: 8140
            cidr_ip: "{{ cidr }}"

   - name: Create IPA Security Group
     ec2_group:
        name: ipa
        description: Rules for IPA directory servers
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 80
            to_port: 80
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 464
            to_port: 464
            cidr_ip: "{{ cidr }}"
          - proto: udp
            from_port: 464
            to_port: 464
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 636
            to_port: 636
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 53
            to_port: 53
            cidr_ip: "{{ cidr }}"
          - proto: udp
            from_port: 53
            to_port: 53
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 389
            to_port: 389
            cidr_ip: "{{ cidr }}"
          - proto: udp
            from_port: 123
            to_port: 123
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 443
            to_port: 443
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 88
            to_port: 88
            cidr_ip: "{{ cidr }}"
          - proto: udp
            from_port: 88
            to_port: 88
            cidr_ip: "{{ cidr }}"

   - name: Start VMs
     with_items: "{{ vms }}"
     ec2:
        exact_count: 1
        count_tag: "{{ item.tags }}"
        instance_type: "{{ item.instance_type }}"
        region: "{{ region }}"
        assign_public_ip: "{{ item.public_ip }}"
        group: "{{ item.security_groups }}"
        key_name: ansible
        user_data: "{{ userdata }}"
        instance_tags: "{{ item.tags }}"
        wait: yes
        vpc_subnet_id: "{{ subnet }}"
        image: "{{ image }}"
        instance_profile_name: "{{ item.iam }}"
     register: instances

   - add_host:
      hostname: "{{ item.tagged_instances[0].private_ip }}"
      groups: "{{ item.item.role }}"
      fqdn: "{{ item.item.fqdn}}"
     with_items: "{{ instances.results }}"   

- name: Install IPA
  hosts: 
   - ipa_master
   - ipa_replica
  gather_facts: false
  tasks:
   - name: Wait for SSH
     local_action: 
      module: wait_for
      port: 22 
      host: "{{ inventory_hostname }}"

   - name: Install IPA
     become: yes
     yum: name={{ item }} state=present
     with_items:
      - ipa-server
      - ipa-server-dns
   - name: Open Firewall
     become: yes
     firewalld: service={{ item }} permanent=true state=enabled immediate=true
     with_items:
      - http
      - https
      - ldap
      - ldaps
      - kerberos
      - kpasswd
      - ntp
      - dns

- name: Configure IPA Master
  hosts:
   - ipa_master
  tasks:
   - name: Run Installer
     become: yes
     command: ipa-server-install --mkhomedir --setup-dns --forwarder={{ dns_forwarder }} --unattended --ds-password={{ ds_password }} --hostname={{ fqdn }} --admin-password={{ admin_password }} --realm={{ realm }} --ip-address={{ inventory_hostname }}
     args:
       creates: /etc/ipa/default.conf

- name: Configure IPA clients
  hosts:
   - ipa_replica
   - foreman
   - ansible
   - jump
  vars:
   ipaserver_ip: "{{ groups.ipa_master[0] }}"
  tasks:

   - name: Set hostname
     become: yes
     hostname:
       name: "{{ fqdn }}"

   - name: Set IPA as DNS
     become: yes
     template:
       src: resolv.conf.j2
       dest: /etc/resolv.conf

   - name: Configure ipa client
     become: yes
     command: ipa-client-install --mkhomedir --principal=admin --password={{ admin_password }} --unattended
     args:
       creates: /etc/ipa/default.conf          

- name: Configure IPA Replica
  hosts:
   - ipa_replica
  tasks:
   - name: Configure replica
     become: yes
     command: ipa-replica-install --setup-ca --setup-dns --forwarder={{ dns_forwarder }} --unattended --password={{ ds_password }} --admin-password={{ admin_password }} --mkhomedir
     args:
       creates: /etc/systemd/system/multi-user.target.wants/ipa.service     

- hosts: foreman
  gather_facts: false
  tasks:
   - name: Install Foreman Repos
     become: yes
     yum: name={{ item }} state=present
     with_items:
      - https://yum.puppetlabs.com/puppetlabs-release-pc1-el-7.noarch.rpm
      - epel-release
      - https://yum.theforeman.org/releases/1.13/el7/x86_64/foreman-release.rpm
  
   - name: Install Foreman Installer
     become: yes
     yum: name={{ item }} state=present
     with_items:
      - foreman-installer
      - ipa-admintools
      - httpd
  
   - name: Trust IPA certificates
     become: yes
     copy:
      remote_src: True
      src: /etc/ipa/ca.crt
      dest: /etc/pki/ca-trust/source/anchors/ca.crt
     register: copy

   - name: Update Trust
     become: yes
     command: update-ca-trust
     when: copy.changed

   - name: Create HTTP Service Principal
     become: yes
     shell: echo "{{ admin_password }}" | kinit admin && ipa service-add "HTTP/{{ fqdn }}" && ipa-getkeytab -k /etc/http.keytab -p "HTTP/{{ fqdn }}"
     args:
      creates: /etc/http.keytab

   - name: Set keytab permissions
     become: yes
     file:
       path: /etc/http.keytab
       owner: apache
       group: apache
       mode: 600
 
   - name: Open Firewall Services
     become: yes
     firewalld: service={{ item }} permanent=true state=enabled immediate=true
     with_items:
      - http
      - https

   - name: Open Firewall Ports
     become: yes
     firewalld: port={{ item }} permanent=true state=enabled immediate=true
     with_items:
      - 8140/tcp
      - 8443/tcp

# Install Foreman
# foreman-installer --foreman-admin-password {{ admin_password }} --enable-foreman-proxy-plugin-openscap --enable-foreman-plugin-openscap --foreman-ipa-authentication=true --foreman-organizations-enabled=true --foreman-locations-enabled=true --foreman-initial-location=us-east-1 --foreman-initial-organization=BAH --enable-foreman-proxy          

   - name: Create realm-proxy
     become: yes
     shell: echo -n "{{ admin_password }}" | foreman-prepare-realm admin realm-proxy && cp freeipa.keytab /etc/foreman-proxy/ && rm -f freeipa.keytab && chown foreman-proxy /etc/foreman-proxy/freeipa.keytab && chmod 600 /etc/foreman-proxy/freeipa.keytab
     args:
      creates: /etc/foreman-proxy/freeipa.keytab

   - name: Install Puppet Modules
     command: puppet module install -i /etc/puppetlabs/code/environments/production/modules {{ item }}
     with_items:
      - puppetlabs/ntp
      - wdijkerman/zabbix
      - saz/resolv_conf
      - jlambert121-yum
      - treydock-yum_cron
      - isimluk-foreman_scap_client
     args:
      creates: /etc/puppetlabs/code/environments/production/modules/foreman_scap_client/manifests/init.pp                            
   
   - name: Configure Hammer
     template:
       src: cli_config.yml.j2
       dest: "/home/{{ ansible_ssh_user }}/.hammer/cli_config.yml"

- hosts: ansible
  gather_facts: false
  tasks:
   - debug: var=fqdn                 
EOF
export ANSIBLE_HOST_KEY_CHECKING=False
ansible-playbook start.yml