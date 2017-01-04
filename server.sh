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

- name: Configure IPA Replica
  hosts:
   - ipa_replica
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

   - name: Configure replica
     become: yes
     command: ipa-replica-install --setup-ca --setup-dns --forwarder={{ dns_forwarder }} --unattended --password={{ ds_password }} --admin-password={{ admin_password }} --mkhomedir
#     args:
#       creates: /etc/ipa/default.conf     

- hosts: foreman
  gather_facts: false
  tasks:
   - debug: var=fqdn

- hosts: ansible
  gather_facts: false
  tasks:
   - debug: var=fqdn                 
EOF
export ANSIBLE_HOST_KEY_CHECKING=False
ansible-playbook start.yml