package org.cuit.epoch.strategy.impl;

import cn.dev33.satoken.stp.StpUtil;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.conditions.update.LambdaUpdateWrapper;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.dto.strategy.login.SocialTokenDTO;
import org.cuit.epoch.dto.strategy.login.SocialUserInfoDTO;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.dto.UserInfoDTO;
import org.cuit.epoch.entity.UserAuth;
import org.cuit.epoch.entity.UserInfo;
import org.cuit.epoch.entity.UserRole;
import org.cuit.epoch.enums.RoleEnum;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.mapper.UserAuthMapper;
import org.cuit.epoch.mapper.UserInfoMapper;
import org.cuit.epoch.mapper.UserRoleMapper;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.service.impl.UserAuthServiceImpl;
import org.cuit.epoch.strategy.SocialLoginStrategy;
import org.cuit.epoch.util.BeanCopyUtils;
import org.cuit.epoch.util.IpUtils;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.Objects;
import java.util.Set;

import static org.cuit.epoch.constant.CommonConst.TRUE;
import static org.cuit.epoch.constant.RedisPrefixConst.*;
import static org.cuit.epoch.constant.RedisPrefixConst.USER_ONLINE;
import static org.cuit.epoch.enums.ZoneEnum.SHANGHAI;

/**
 * @author: ladidol
 * @date: 2022/11/22 16:12
 * @description:
 */
@Slf4j
@Service
public abstract class AbstractSocialLoginStrategyImpl implements SocialLoginStrategy {
    @Autowired
    private UserAuthMapper userAuthDao;
    @Autowired
    private UserInfoMapper userInfoDao;
    @Autowired
    private UserRoleMapper userRoleDao;
    @Autowired
    private UserAuthServiceImpl userAuthService;
    @Resource
    private HttpServletRequest request;
    @Autowired
    RedisService redisService;

    @Override
    public UserInfoDTO login(String data) {
        // 创建登录信息
        UserDetailDTO userDetailDTO;
        // 获取第三方token信息
        SocialTokenDTO socialToken = getSocialToken(data);
        // 获取用户ip信息
        String ipAddress = IpUtils.getIpAddress(request);
        String ipSource = IpUtils.getIpSource(ipAddress);
        // 判断是否已注册
        UserAuth user = getUserAuth(socialToken);
        if (Objects.nonNull(user)) {
            // 返回数据库用户信息
            userDetailDTO = getUserDetail(user, ipAddress, ipSource);
        } else {
            // 获取第三方用户信息，保存到数据库返回
            userDetailDTO = saveUserDetail(socialToken, ipAddress, ipSource);
        }
        // 判断账号是否禁用
        if (userDetailDTO.getIsDisable().equals(TRUE)) {
            throw new AppException("账号已被禁用");
        }
//        // 将登录信息放入springSecurity管理
//        UsernamePasswordAuthenticationToken auth = new UsernamePasswordAuthenticationToken(userDetailDTO, null, userDetailDTO.getAuthorities());
//        SecurityContextHolder.getContext().setAuthentication(auth);
        // 2022/11/22 这里可以将登录信息放到Sa-token中管理
        //sa-token登录
        StpUtil.login(userDetailDTO.getId());
        //将用户角色信息存入session中
        StpUtil.getSession().set(USER_ROLE,userDetailDTO.getRoleList());
        //将用户详细信息存入session中
        StpUtil.getSession().set(USER_INFO,userDetailDTO);
        //将用户UserInfo存到redis中，方便后序对在线人数进行判断
        Set<UserDetailDTO> onlineUsers = (Set<UserDetailDTO>) redisService.get(USER_ONLINE);
        onlineUsers.add(userDetailDTO);
        for (UserDetailDTO onlineUser : onlineUsers) {
            log.info("onlineUser = " + onlineUser);
        }
        redisService.set(USER_ONLINE, onlineUsers);



        // 返回用户信息
        return BeanCopyUtils.copyObject(userDetailDTO, UserInfoDTO.class);
    }

    /**
     * 获取第三方token信息
     *
     * @param data 数据
     * @return {@link SocialTokenDTO} 第三方token信息
     */
    public abstract SocialTokenDTO getSocialToken(String data);

    /**
     * 获取第三方用户信息
     *
     * @param socialTokenDTO 第三方token信息
     * @return {@link SocialUserInfoDTO} 第三方用户信息
     */
    public abstract SocialUserInfoDTO getSocialUserInfo(SocialTokenDTO socialTokenDTO);

    /**
     * 获取用户账号
     *
     * @return {@link UserAuth} 用户账号
     */
    private UserAuth getUserAuth(SocialTokenDTO socialTokenDTO) {
        return userAuthDao.selectOne(new LambdaQueryWrapper<UserAuth>()
                .eq(UserAuth::getUsername, socialTokenDTO.getOpenId())
                .eq(UserAuth::getLoginType, socialTokenDTO.getLoginType()));
    }

    /**
     * 获取用户信息
     *
     * @param user      用户账号
     * @param ipAddress ip地址
     * @param ipSource  ip源
     * @return {@link UserDetailDTO} 用户信息
     */
    private UserDetailDTO getUserDetail(UserAuth user, String ipAddress, String ipSource) {
        // 更新登录信息
        userAuthDao.update(new UserAuth(), new LambdaUpdateWrapper<UserAuth>()
                .set(UserAuth::getLastLoginTime, LocalDateTime.now())
                .set(UserAuth::getIpAddress, ipAddress)
                .set(UserAuth::getIpSource, ipSource)
                .eq(UserAuth::getId, user.getId()));
        // 封装信息
        return userAuthService.convertUserDetail(user, request);
    }

    /**
     * 新增用户信息
     *
     * @param socialToken token信息
     * @param ipAddress   ip地址
     * @param ipSource    ip源
     * @return {@link UserDetailDTO} 用户信息
     */
    private UserDetailDTO saveUserDetail(SocialTokenDTO socialToken, String ipAddress, String ipSource) {
        // 获取第三方用户信息
        SocialUserInfoDTO socialUserInfo = getSocialUserInfo(socialToken);
        // 保存用户信息
        UserInfo userInfo = UserInfo.builder()
                .nickname(socialUserInfo.getNickname())
                .avatar(socialUserInfo.getAvatar())
                .build();
        userInfoDao.insert(userInfo);
        // 保存账号信息
        UserAuth userAuth = UserAuth.builder()
                .userInfoId(userInfo.getId())
                .username(socialToken.getOpenId())
                .password(socialToken.getAccessToken())
                .loginType(socialToken.getLoginType())
                .lastLoginTime(LocalDateTime.now(ZoneId.of(SHANGHAI.getZone())))
                .ipAddress(ipAddress)
                .ipSource(ipSource)
                .build();
        userAuthDao.insert(userAuth);
        // 绑定角色
        UserRole userRole = UserRole.builder()
                .userId(userInfo.getId())
                .roleId(RoleEnum.USER.getRoleId())
                .build();
        userRoleDao.insert(userRole);
        return userAuthService.convertUserDetail(userAuth, request);
    }

}