package com.demo.library.member;

import jakarta.persistence.Entity;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.Id;

@Entity
public class Member {
    @Id
    @GeneratedValue
    private Long id;
    private String email;
    private String plan;

    protected Member() {
    }

    public Member(String email, String plan) {
        this.email = email;
        this.plan = plan;
    }

    public Long getId() { return id; }
    public String getEmail() { return email; }
    public String getPlan() { return plan; }
}
